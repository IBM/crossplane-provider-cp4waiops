/*
Copyright 2020 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cp4waiops

import (
	"bytes"
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"strconv"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.ibm.com/jbjhuang/cloudpak-provider/apis/cp4waiops/v1alpha1"
	apisv1alpha1 "github.ibm.com/jbjhuang/cloudpak-provider/apis/v1alpha1"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	openshiftv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	ocpoperatorv1alpha1Client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1alpha1"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	knativ1alpha1 "knative.dev/operator/pkg/apis/operator/v1alpha1"
	knativeclient "knative.dev/operator/pkg/client/clientset/versioned/typed/operator/v1alpha1"

	operatorclient "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	//localstoragev1 "github.com/openshift/local-storage-operator/api/v1"
)

const (
	errNotCp4waiops      = "managed resource is not a Cp4waiops custom resource"
	errTrackPCUsage      = "cannot track ProviderConfig usage"
	errGetPC             = "cannot get ProviderConfig"
	errGetCreds          = "cannot get credentials"
	errObserveCp4waiops  = "observe cp4waiops error"
	errCreateCp4waiops   = "create cp4waiops error"
	errUnmarshalTemplate = "cannot unmarshal template"

	errNewClient = "cannot create new Service"

	DGlobalImagePullSecret  = "GlobalImagePullSecret"
	DNamespace              = "Namespace"
	DImageContentPolicy     = "ImageContentPolicy"
	DImageContentPolicyName = "mirror-config"
	DImagePullSecret        = "ImagePullSecret"
	DStrimzOperator         = "StrimzOperator"
	DStrimzOperatorName     = "strimzi-kafka-operator"
	DOpenshiftOperatorNS    = "openshift-operators"
	DServerlessOperator     = "ServerlessOperator"
	DServerlessOperatorName = "serverless-operator"
	DServerlessNamespace    = "ServerlessNamespace"
	DKnativeServingInstance = "KnativeServingInstance"
	DKnativeEveningInstance = "KnativeEveningInstance"

	CatalogSource    = "CatalogSource"
	OCPMarketplaceNS = "openshift-marketplace"

	AIOpsSubscription     = "AIOpsSubscription"
	OCPOperatorNS         = "openshift-operators"
	AIOpsSubscriptionName = "ibm-aiops-orchestrator"

	KNATIVE_SERVING_NAMESPACE  = "knative-serving"
	KNATIVE_EVENTING_NAMESPACE = "knative-eventing"

	KNATIVE_SERVING_INSTANCE_NAME  = "knative-serving"
	KNATIVE_EVENTING_INSTANCE_NAME = "knative-eventing"

	//Global image pull secret
	GBSNamespace  = "openshift-config"
	internalEmail = "tester@ibm.com"

	//OCS
	OCSNamespace = "openshift-storage"
	DOCS         = "OCS"

	//CP4WAIOPS
	DWAIOPS = "cp4waiops"

	Validate = "validate"
)

//Pull secret begin
type Auths struct {
	Auths map[string]interface{} `json:"auths"`
}

type Registry struct {
	Auth  string `json:"auth"`
	Email string `json:"email"`
}

//Pull secret end

// A NoOpService does nothing.
type NoOpService struct{}

var (
	newNoOpService = func(_ []byte) (interface{}, error) { return &NoOpService{}, nil }

	registries = map[string][]string{
		"cp.icr.io/cp":        {"cp.stg.icr.io/cp"},
		"docker.io/ibmcom":    {"cp.stg.icr.io/cp"},
		"quay.io/opencloudio": {"hyc-cloud-private-daily-docker-local.artifactory.swg-devops.com/ibmcom"},
		"cp.icr.io/cp/cpd":    {"hyc-cloud-private-daily-docker-local.artifactory.swg-devops.com/ibmcom"},
	}
	component = ""

	internalArtifactories = [...]string{
		"hyc-katamari-cicd-team-docker-local.artifactory.swg-devops.com",
		"hyc-cloud-private-daily-docker-local.artifactory.swg-devops.com",
		"hyc-cp4mcm-team-docker-local.artifactory.swg-devops.com",
		"orpheus-local-docker.artifactory.swg-devops.com",
	}

	aiopsNamespace       = "aiops"
	DImagePullSecretName = "ibm-entitlement-key"
)

// Setup adds a controller that reconciles Dependency managed resources.
func Setup(mgr ctrl.Manager, l logging.Logger, rl workqueue.RateLimiter) error {
	name := managed.ControllerName(v1alpha1.Cp4waiopsGroupKind)
	logger := l.WithValues("controller", name)

	o := controller.Options{
		RateLimiter: ratelimiter.NewDefaultManagedRateLimiter(rl),
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.Cp4waiopsGroupVersionKind),
		managed.WithExternalConnecter(&connector{
			logger: logger,
			kube:   mgr.GetClient(),
			usage:  resource.NewProviderConfigUsageTracker(mgr.GetClient(), &apisv1alpha1.ProviderConfigUsage{}),
		}),
		managed.WithLogger(l.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o).
		For(&v1alpha1.Cp4waiops{}).
		Complete(r)
}

// A connector is expected to produce an ExternalClient when its Connect method
// is called.
type connector struct {
	logger logging.Logger
	kube   client.Client
	usage  resource.Tracker
}

// Connect typically produces an ExternalClient by:
// 1. Tracking that the managed resource is using a ProviderConfig.
// 2. Getting the managed resource's ProviderConfig.
// 3. Getting the credentials specified by the ProviderConfig.
// 4. Using the credentials to form a client.
func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.Cp4waiops)
	if !ok {
		return nil, errors.New(errNotCp4waiops)
	}

	logger := c.logger.WithValues("request", cr.Name)

	logger.Info("Connecting")

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &apisv1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	if data == nil || len(data) == 0 {
		return nil, errors.New("The secret is not ready yet")
	}

	clientConfig, err := clientcmd.RESTConfigFromKubeConfig(data)

	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		panic(err)
	}

	opClientInstance, err := operatorclient.NewForConfig(clientConfig)
	if err != nil {
		panic(err)
	}

	//Create a external client.Client for generic CR management
	kc, err := client.New(clientConfig, client.Options{})
	if err != nil {
		return nil, errors.Wrap(err, "cannot create Kubernetes client")
	}

	//Get the imagepullsecret data in same namespace
	/*
		cd.CommonCredentialSelectors.SecretRef.Name = DImagePullSecretName
		data, err = resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	*/
	return &external{
		logger:     logger,
		localKube:  c.kube,
		kube:       kubeClient,
		config:     clientConfig,
		opClient:   opClientInstance,
		kubeclient: kc,
		//		pullsecret: data,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	logger     logging.Logger
	localKube  client.Client
	kube       *kubernetes.Clientset
	config     *rest.Config
	opClient   *operatorclient.Clientset
	kubeclient client.Client
	//	pullsecret []byte
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.Cp4waiops)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotCp4waiops)
	}

	// These fmt statements should be removed in the real implementation.
	e.logger.Info("Observing: " + cr.ObjectMeta.Name)

	//this is for test
	//err := e.observeTest()

	//Check aiops namespace
	/*
		err := e.observeOCS(ctx)
		if err != nil {
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
	*/
	//Check aiops namespace
	err := e.observeNamespace(ctx, cr)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check image content policy
	/*
		err = e.observeImageContentPolicy(ctx)
		if err != nil {
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
	*/

	//Patch global image pull secret
	/*
		err = e.observeGlobalImagePullSecret(ctx)
		if err != nil {
			return managed.ExternalObservation{
				ResourceExists: false,
			}, nil
		}
	*/

	//Check image pull secret
	err = e.observeImagePullSecret(ctx, cr)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check Strimz operator
	err = e.observeStrimzOperator(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check Serverless operator
	err = e.observeServerlessOperator(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check Knative installing namespace
	err = e.observeServerlessNamespace(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check Knative Serving instance
	err = e.observeKnativeServingInstance(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check Knative Eventing instance
	err = e.observeKnativeEventingInstance(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check Catalog resources
	err = e.observeCatalogSources(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check AIOPS subscription
	err = e.observeAIOpsSubscription(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Check AIOPS subscription
	err = e.observeCP4WAIOPS(ctx, cr)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	//Fix some errors while installting aiops
	err = e.validateCP4WAIOPS(ctx)
	if err != nil {
		return managed.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalObservation{
		// Return false when the external resource does not exist. This lets
		// the managed resource reconciler know that it needs to call Create to
		// (re)create the resource, or that it has successfully been deleted.
		ResourceExists: true,

		// Return false when the external resource exists, but it not up to date
		// with the desired managed resource state. This lets the managed
		// resource reconciler know that it needs to call Update.
		ResourceUpToDate: true,

		// Return any details that may be required to connect to the external
		// resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	cr, ok := mg.(*v1alpha1.Cp4waiops)
	if !ok {
		return managed.ExternalCreation{}, errors.New(errNotCp4waiops)
	}

	e.logger.Info("Creating: " + cr.ObjectMeta.Name)
	e.logger.Info("current stage : " + component)
	//Only handle unavailable resources/services/deployment
	switch {
	case component == DOCS:
		e.createOCS(ctx)
	case component == DNamespace:
		e.createNamespace(ctx)
	case component == DGlobalImagePullSecret:
		e.patchGlobalImagePullSecret(ctx)
	case component == DImageContentPolicy:
		e.createImageContentPolicy(ctx)
	case component == DImagePullSecret:
		e.createImagePullSecret(ctx)
	case component == DStrimzOperator:
		e.createStrimzOperator(ctx)
	case component == DServerlessOperator:
		e.createServerlessOperator(ctx)
	case component == DServerlessNamespace:
		e.createServerlessNamespace(ctx)
	case component == DKnativeServingInstance:
		e.createKnativeServingInstance(ctx)
	case component == DKnativeEveningInstance:
		e.createKnativeEventingInstance(ctx)
	case component == CatalogSource:
		e.createCatalogSources(ctx, cr)
	case component == AIOpsSubscription:
		e.createAIOpsSubscription(ctx, cr)
	case component == DWAIOPS:
		e.createCP4WAIOPS(ctx, cr)
	}

	return managed.ExternalCreation{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.Cp4waiops)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotCp4waiops)
	}

	e.logger.Info("Updating: " + cr.ObjectMeta.Name)

	return managed.ExternalUpdate{
		// Optionally return any details that may be required to connect to the
		// external resource. These will be stored as the connection secret.
		ConnectionDetails: managed.ConnectionDetails{},
	}, nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.Cp4waiops)
	if !ok {
		return errors.New(errNotCp4waiops)
	}

	e.logger.Info("Deleting: " + cr.ObjectMeta.Name)

	return nil
}

func int32Ptr(i int32) *int32 { return &i }

func (e *external) observeNamespace(ctx context.Context, cr *v1alpha1.Cp4waiops) error {
	component = DNamespace
	aiopsNamespace = cr.Spec.ForProvider.InstallParams.Namespace
	e.logger.Info("Observe Namespace existing for aiops " + aiopsNamespace)

	_, err := e.kube.CoreV1().Namespaces().Get(context.TODO(), aiopsNamespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	//component = DImageContentPolicy

	return nil
}

func (e *external) createNamespace(ctx context.Context) error {
	e.logger.Info("Creating Namespace for aiops " + aiopsNamespace)
	namespaceSource := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: aiopsNamespace,
		},
	}
	namespaceobj, err := e.kube.CoreV1().Namespaces().Create(context.TODO(), namespaceSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create namespace error , namespace : " + aiopsNamespace)
		return err
	}
	e.logger.Info("namespace created " + namespaceobj.Name)
	return nil
}

func (e *external) observeImageContentPolicy(ctx context.Context) error {

	e.logger.Info("Observe ImageContentPolicy existing for aiops ")

	openshiftClient, err := ocpoperatorv1alpha1Client.NewForConfig(e.config)
	if err != nil {
		e.logger.Info("Create ocp client error")
		return err
	}
	_, err = openshiftClient.ImageContentSourcePolicies().Get(ctx, DImageContentPolicyName, metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Get ImageContentPolicy error")
		return err
	}
	component = DImagePullSecret
	return nil
}

func (e *external) createImageContentPolicy(ctx context.Context) error {

	e.logger.Info("Create ImageContentPolicy for aiops ")

	openshiftClient, err := ocpoperatorv1alpha1Client.NewForConfig(e.config)
	if err != nil {
		e.logger.Info("Create ocp client error")
		return err
	}

	mirrors := []openshiftv1alpha1.RepositoryDigestMirrors{}
	for source, mirror := range registries {
		mirrors = append(mirrors, openshiftv1alpha1.RepositoryDigestMirrors{
			Source:  source,
			Mirrors: mirror,
		})
	}

	imagepolicy := &openshiftv1alpha1.ImageContentSourcePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: DImageContentPolicyName,
		},
		Spec: openshiftv1alpha1.ImageContentSourcePolicySpec{
			RepositoryDigestMirrors: mirrors,
		},
	}

	imageContentSourcePolicy, err := openshiftClient.ImageContentSourcePolicies().Create(ctx, imagepolicy, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("Create imageContentSourcePolicy error")
		return err
	}
	e.logger.Info("ImageContentSourcePolicy created : " + imageContentSourcePolicy.Name)
	return nil
}

func (e *external) observeGlobalImagePullSecret(ctx context.Context) error {
	component = DGlobalImagePullSecret
	e.logger.Info("Observe Global image secret existing for aiops ")

	secretobj, err := e.kube.CoreV1().Secrets(GBSNamespace).Get(ctx, "pull-secret", metav1.GetOptions{})
	if err != nil {
		return err
	}

	content := secretobj.Data[".dockerconfigjson"]
	//	e.logger.Info("data is : " + string(content))
	auths := &Auths{}
	err = json.Unmarshal(content, &auths)
	if err != nil {
		e.logger.Info("unmarshal content error , content :" + string(content))
	}
	/*
		artifactories := auths.Auths
		for url, _ := range artifactories {
			e.logger.Info("artifactory is :" + url)
		}
	*/

	if _, ok := auths.Auths["cp.icr.io"]; !ok {
		return errors.New("Pull image doesn't meet requirement , lack of artifactory cp.icr.io")
	}

	if _, ok := auths.Auths["cp.stg.icr.io"]; !ok {
		return errors.New("Pull image doesn't meet requirement , lack of artifactory cp.stg.icr.io")
	}

	if _, ok := auths.Auths["docker.io"]; !ok {
		return errors.New("Pull image doesn't meet requirement , lack of artifactory docker.io")
	}

	if _, ok := auths.Auths["hyc-katamari-cicd-team-docker-local.artifactory.swg-devops.com"]; !ok {
		return errors.New("Pull image doesn't meet requirement , lack of artifactory hyc-katamari-cicd-team-docker-local.artifactory.swg-devops.com/ibmcom/aiops-orchestrator-catalog")
	}

	return nil
}

func (e *external) patchGlobalImagePullSecret(ctx context.Context) error {
	e.logger.Info("Patching ImagePullSecret for aiops ")

	//Get local credentials setting from secret global-secret
	credentials := &corev1.Secret{}
	err := e.localKube.Get(ctx, types.NamespacedName{Name: "global-secret", Namespace: "crossplane-system"}, credentials)
	if err != nil {
		e.logger.Info("ERROR, failed to get secret global-secret from namespace crossplane-system , patch global image pull secret error ")
		return err
	}

	cpIcrIoAuth, ok := credentials.Data["cpIcrIoAuth"]
	if !ok {
		e.logger.Info("ERROR, failed to get key cpIcrIoAuth from secret global-secret , namespace crossplane-system , patch global image pull secret error ")
		return err
	}

	cpStgIcrIoAuth, ok := credentials.Data["cpStgIcrIoAuth"]
	if !ok {
		e.logger.Info("ERROR, failed to get key cpStgIcrIoAuth from secret global-secret , namespace crossplane-system , patch global image pull secret error ")
		return err
	}

	dockerioAuth, ok := credentials.Data["dockerioAuth"]
	if !ok {
		e.logger.Info("ERROR, failed to get key dockerioAuth from secret global-secret , namespace crossplane-system , patch global image pull secret error ")
		return err
	}

	interalAuth, ok := credentials.Data["interalAuth"]
	if !ok {
		e.logger.Info("ERROR, failed to get key interalAuth from secret global-secret , namespace crossplane-system , patch global image pull secret error ")
		return err
	}

	secretobj, err := e.kube.CoreV1().Secrets(GBSNamespace).Get(ctx, "pull-secret", metav1.GetOptions{})
	if err != nil {
		return err
	}

	content := secretobj.Data[".dockerconfigjson"]
	auths := &Auths{}
	err = json.Unmarshal(content, &auths)
	if err != nil {
		e.logger.Info("unmarshal content error , content :" + string(content))
	}
	e.logger.Info("Origin imagePullSecret  in namespace " + GBSNamespace + ", dockerconfig :" + string(content))
	for _, artifactory := range internalArtifactories {
		e.addSecret(auths, artifactory, interalAuth, internalEmail)
	}
	e.addSecret(auths, "cp.icr.io", cpIcrIoAuth, internalEmail)
	e.addSecret(auths, "cp.stg.icr.io", cpStgIcrIoAuth, internalEmail)
	e.addSecret(auths, "docker.io", dockerioAuth, "tester@ibm.com")

	data, err := json.Marshal(auths)
	if err != nil {
		e.logger.Info("marshal auth json error")
	}
	secretobj.Data[".dockerconfigjson"] = data

	//secretobj, err = e.kube.CoreV1().Secrets(GBSNamespace).Patch(context.TODO(), secretobj.Name, types.MergePatchType, data, metav1.PatchOptions{})
	secretobj, err = e.kube.CoreV1().Secrets(GBSNamespace).Update(context.TODO(), secretobj, metav1.UpdateOptions{})
	if err != nil {
		e.logger.Info("Patch secret error , namespace : " + GBSNamespace + ", name :" + secretobj.Name)
		return err
	}
	e.logger.Info("imagePullSecret Updated in namespace " + GBSNamespace + ", name :" + secretobj.Name + ", dockerconfig :" + string(secretobj.Data[".dockerconfigjson"]))
	return nil
}

func (e *external) observeImagePullSecret(ctx context.Context, cr *v1alpha1.Cp4waiops) error {
	component = DImagePullSecret
	DImagePullSecretName = cr.Spec.ForProvider.InstallParams.ImagePullSecret
	e.logger.Info("Observe ImagePullSecret existing for aiops ")

	_, err := e.kube.CoreV1().Secrets(aiopsNamespace).Get(ctx, DImagePullSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	_, err = e.kube.CoreV1().Secrets(OCPMarketplaceNS).Get(ctx, DImagePullSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	_, err = e.kube.CoreV1().Secrets(OCPOperatorNS).Get(ctx, DImagePullSecretName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	//workaround , the catalogsource hard-code the image pull secret with "ibm-aiops-pull-secret"
	_, err = e.kube.CoreV1().Secrets(OCPOperatorNS).Get(ctx, "ibm-aiops-pull-secret", metav1.GetOptions{})
	if err != nil {
		return err
	}

	//need patch the default serviceaccount in openshift-marketplace namespace
	sa, err := e.kube.CoreV1().ServiceAccounts(OCPMarketplaceNS).Get(ctx, "default", metav1.GetOptions{})
	if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
		return errors.New("Error , no imagepullsecret patched to serviceaccount default , namespace " + OCPMarketplaceNS)
	}

	//need patch the strimzi-cluster-operator serviceaccount in openshift-operator namespace
	sa, err = e.kube.CoreV1().ServiceAccounts(OCPOperatorNS).Get(ctx, "strimzi-cluster-operator", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("The serviceaccount strimzi-cluster-operator in namespace " + OCPOperatorNS + " has not been created yet")
		return nil
	} else {
		if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
			return errors.New("Error , no imagepullsecret patched to serviceaccount strimzi-cluster-operator , namespace " + OCPOperatorNS)
		}
	}

	//need patch the ibm-aiops-orchestrator serviceaccount in openshift-operator namespace
	sa, err = e.kube.CoreV1().ServiceAccounts(OCPOperatorNS).Get(ctx, "ibm-aiops-orchestrator", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("The serviceaccount ibm-aiops-orchestrator in namespace " + OCPOperatorNS + " has not been created yet")
		return nil
	} else {
		if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
			return errors.New("Error , no imagepullsecret patched to serviceaccount ibm-aiops-orchestrator , namespace " + OCPOperatorNS)
		}
	}

	//need patch the strimzi-cluster-zookeeper serviceaccount in aiops namespace
	sa, err = e.kube.CoreV1().ServiceAccounts(aiopsNamespace).Get(ctx, "strimzi-cluster-zookeeper", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("The serviceaccount strimzi-cluster-zookeeper in namespace " + aiopsNamespace + " has not been created yet")
		return nil
	} else {
		if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
			return errors.New("Error , no imagepullsecret patched to serviceaccount strimzi-cluster-zookeeper , namespace " + aiopsNamespace)
		}
	}

	//need patch the strimzi-cluster-kafka serviceaccount in aiops namespace
	sa, err = e.kube.CoreV1().ServiceAccounts(aiopsNamespace).Get(ctx, "strimzi-cluster-kafka", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("The serviceaccount strimzi-cluster-kafka in namespace " + aiopsNamespace + " has not been created yet")
		return nil
	} else {
		if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
			return errors.New("Error , no imagepullsecret patched to serviceaccount strimzi-cluster-kafka , namespace " + aiopsNamespace)
		}
	}

	//need patch the strimzi-cluster-entity-operator serviceaccount in aiops namespace
	sa, err = e.kube.CoreV1().ServiceAccounts(aiopsNamespace).Get(ctx, "strimzi-cluster-entity-operator", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("The serviceaccount strimzi-cluster-entity-operator in namespace " + aiopsNamespace + " has not been created yet")
		return nil
	} else {
		if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
			return errors.New("Error , no imagepullsecret patched to serviceaccount strimzi-cluster-entity-operator , namespace " + aiopsNamespace)
		}
	}

	return nil
}

func contains(s []corev1.LocalObjectReference, str string) bool {
	for _, v := range s {
		if v.Name == str {
			return true
		}
	}
	return false
}

func (e *external) handleImagePullSecret(ctx context.Context) ([]byte, error) {
	e.logger.Info("Handling ImagePullSecret for aiops ")
	//Get local credentials setting from secret global-secret
	credentials := &corev1.Secret{}
	err := e.localKube.Get(ctx, types.NamespacedName{Name: "image-pull-secret", Namespace: "crossplane-system"}, credentials)
	if err != nil {
		e.logger.Info("ERROR, failed to get secret image-pull-secret from namespace crossplane-system , handling image pull secret error ")
		return nil, err
	}
	auths := &Auths{}
	auths.Auths = make(map[string]interface{})
	for key, auth := range credentials.Data {
		e.addSecret(auths, key, auth, internalEmail)
	}
	data, err := json.Marshal(auths)
	if err != nil {
		e.logger.Info("marshal auth json error")
		return nil, err
	}

	return data, nil
}

func (e *external) createImagePullSecret(ctx context.Context) error {
	e.logger.Info("Creating ImagePullSecret for aiops ")

	//Read customized image pull secret
	pullsecret, err := e.handleImagePullSecret(ctx)
	if err != nil {
		e.logger.Info("ERROR , handling customized image pull secret failed")
		return err
	}
	secretSource := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: DImagePullSecretName,
		},
		Data: map[string][]byte{
			".dockerconfigjson": pullsecret,
		},
		Type: "kubernetes.io/dockerconfigjson",
	}
	//workaround for catalogsource , it hard-code the image pull secret "ibm-aiops-pull-secret"
	secretWorkaround := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ibm-aiops-pull-secret",
		},
		Data: map[string][]byte{
			".dockerconfigjson": pullsecret,
		},
		Type: "kubernetes.io/dockerconfigjson",
	}
	secretWorkaroundobj, err := e.kube.CoreV1().Secrets(OCPOperatorNS).Create(context.TODO(), &secretWorkaround, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create imagePullSecret ibm-aiops-pull-secret error , namespace : " + OCPOperatorNS)
		return err
	}
	e.logger.Info("imagePullSecret created in namespace " + OCPOperatorNS + ", name :" + secretWorkaroundobj.Name)

	secretobj, err := e.kube.CoreV1().Secrets(aiopsNamespace).Create(context.TODO(), &secretSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create imagePullSecret error , namespace : " + aiopsNamespace)
		return err
	}
	e.logger.Info("imagePullSecret created in namespace " + aiopsNamespace + ", name :" + secretobj.Name)

	secretobj, err = e.kube.CoreV1().Secrets(OCPMarketplaceNS).Create(context.TODO(), &secretSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create imagePullSecret error , namespace : " + OCPMarketplaceNS + ", name :" + DImagePullSecretName)
		return err
	}
	e.logger.Info("imagePullSecret created in namespace " + OCPMarketplaceNS + ", name :" + secretobj.Name)

	secretobj, err = e.kube.CoreV1().Secrets(OCPOperatorNS).Create(context.TODO(), &secretSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create imagePullSecret error , namespace : " + OCPOperatorNS + ", name :" + DImagePullSecretName)
		return err
	}
	e.logger.Info("imagePullSecret created in namespace " + OCPMarketplaceNS + ", name :" + secretobj.Name)

	err = e.patchServiceaccount(ctx, OCPMarketplaceNS, "default")
	if err != nil {
		return err
	}

	err = e.patchServiceaccount(ctx, OCPOperatorNS, "strimzi-cluster-operator")
	if err != nil {
		return err
	}

	err = e.patchServiceaccount(ctx, OCPOperatorNS, "ibm-aiops-orchestrator")
	if err != nil {
		return err
	}

	err = e.patchServiceaccount(ctx, aiopsNamespace, "strimzi-cluster-zookeeper")
	if err != nil {
		return err
	}

	err = e.patchServiceaccount(ctx, aiopsNamespace, "strimzi-cluster-kafka")
	if err != nil {
		return err
	}

	err = e.patchServiceaccount(ctx, aiopsNamespace, "strimzi-cluster-entity-operator")
	if err != nil {
		return err
	}

	return nil
}

func (e *external) patchServiceaccount(ctx context.Context, namespace string, serviceaccount string) error {
	e.logger.Info("Patching ImagePullSecret for needed serviceaccount ")
	sa, err := e.kube.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), serviceaccount, metav1.GetOptions{})
	if err != nil {
		e.logger.Info("The serviceaccount " + serviceaccount + " in namespace " + namespace + " has not been created yet")
		return nil
	}
	//If the SA is already patched , then just return
	if contains(sa.ImagePullSecrets, DImagePullSecretName) {
		return nil
	}
	sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
		Name: DImagePullSecretName,
	})
	sa, err = e.kube.CoreV1().ServiceAccounts(namespace).Update(context.TODO(), sa, metav1.UpdateOptions{})
	if err != nil {
		e.logger.Info("ERROR , Update serviceaccount " + serviceaccount + " in namespace " + namespace + " failed")
		return err
	}
	if !contains(sa.ImagePullSecrets, DImagePullSecretName) {
		return errors.New("Error , no imagepullsecret patched to serviceaccount" + serviceaccount + " in namespace " + namespace)
	}
	return nil
}

func (e *external) observeStrimzOperator(ctx context.Context) error {
	component = DStrimzOperator
	e.logger.Info("Observe StrimzOperator existing for aiops ")

	opaiops, err := e.opClient.OperatorsV1alpha1().
		Subscriptions(DOpenshiftOperatorNS).
		Get(ctx, DStrimzOperatorName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !(opaiops.Status.State == "AtLatestKnown") {
		//Return reconcile waiting for StrimzOperator ready
		e.logger.Info("Waiting for strimzi-kafka-operator operator AtLatestKnown")
		component = DStrimzOperator
		return nil
	}

	return nil
}

func (e *external) createStrimzOperator(ctx context.Context) error {
	e.logger.Info("Creating StrimzOperator for aiops ")
	subscription := &operatorv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: DOpenshiftOperatorNS,
			Name:      DStrimzOperatorName,
		},
		Spec: &operatorv1alpha1.SubscriptionSpec{
			Channel:                "strimzi-0.19.x",
			InstallPlanApproval:    operatorv1alpha1.ApprovalAutomatic,
			CatalogSource:          "community-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			Package:                "strimzi-kafka-operator",
		},
	}
	opStrimzi, err := e.opClient.OperatorsV1alpha1().
		Subscriptions("openshift-operators").
		Create(ctx, subscription, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	e.logger.Info("StrimzOperator subscription created " + opStrimzi.Name)
	return nil
}

func (e *external) observeServerlessOperator(ctx context.Context) error {
	component = DServerlessOperator
	e.logger.Info("Observe ServerlessOperator existing for aiops ")

	opaiops, err := e.opClient.OperatorsV1alpha1().
		Subscriptions(DOpenshiftOperatorNS).
		Get(ctx, DServerlessOperatorName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !(opaiops.Status.State == "AtLatestKnown") {
		//Return reconcile waiting for StrimzOperator ready
		e.logger.Info("Waiting for Serverless Operator AtLatestKnown")
		/*
			//if pod is not ready , could check pods in openshift-operators
				e.logger.Info("Troubleshooting , Operator not ready ")
				_, err = e.kube.CoreV1().Pods(aiopsNamespace).Get(context.TODO(), KNATIVE_EVENTING_NAMESPACE, metav1.GetOptions{})
				if err != nil {
					return err
				}
		*/
		component = DServerlessOperator
		return nil
	}

	return nil
}

func (e *external) createServerlessOperator(ctx context.Context) error {
	e.logger.Info("Creating ServerlessOperator for aiops ")

	subscription := &operatorv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: DOpenshiftOperatorNS,
			Name:      DServerlessOperatorName,
		},
		Spec: &operatorv1alpha1.SubscriptionSpec{
			Channel:                "4.6",
			InstallPlanApproval:    operatorv1alpha1.ApprovalAutomatic,
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
			Package:                "serverless-operator",
		},
	}
	opServerless, err := e.opClient.OperatorsV1alpha1().
		Subscriptions(DOpenshiftOperatorNS).
		Create(ctx, subscription, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	e.logger.Info("StrimzOperator subscription created " + opServerless.Name)
	return nil
}

func (e *external) observeServerlessNamespace(ctx context.Context) error {
	component = DServerlessNamespace
	e.logger.Info("Observe ServerlessNamespace existing for aiops " + KNATIVE_SERVING_NAMESPACE)

	_, err := e.kube.CoreV1().Namespaces().Get(context.TODO(), KNATIVE_SERVING_NAMESPACE, metav1.GetOptions{})
	if err != nil {
		return err
	}

	e.logger.Info("Observe ServerlessNamespace existing for aiops " + KNATIVE_EVENTING_NAMESPACE)
	_, err = e.kube.CoreV1().Namespaces().Get(context.TODO(), KNATIVE_EVENTING_NAMESPACE, metav1.GetOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (e *external) createServerlessNamespace(ctx context.Context) error {
	e.logger.Info("Creating Namespace for aiops " + KNATIVE_SERVING_NAMESPACE)
	namespaceSource := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: KNATIVE_SERVING_NAMESPACE,
		},
	}
	namespaceobj, err := e.kube.CoreV1().Namespaces().Create(context.TODO(), namespaceSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create namespace error , namespace : " + namespaceobj.Name)
		return err
	}

	e.logger.Info("Creating Namespace for aiops " + KNATIVE_EVENTING_NAMESPACE)
	namespaceSource = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: KNATIVE_EVENTING_NAMESPACE,
		},
	}
	namespaceobj, err = e.kube.CoreV1().Namespaces().Create(context.TODO(), namespaceSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create namespace error , namespace : " + namespaceobj.Name)
		return err
	}

	e.logger.Info("Knative namespaces created ")
	return nil
}

func (e *external) observeKnativeServingInstance(ctx context.Context) error {
	component = DKnativeServingInstance
	e.logger.Info("Observe KnativeServingInstance existing for aiops ")

	knativeClient, err := knativeclient.NewForConfig(e.config)
	if err != nil {
		return nil
	}
	knservingInstance, err := knativeClient.KnativeServings(KNATIVE_SERVING_NAMESPACE).Get(ctx, KNATIVE_SERVING_INSTANCE_NAME, metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to list KnativeServingInstance , Namespace: " + KNATIVE_SERVING_NAMESPACE + ", KnativeServerInstance :" + KNATIVE_SERVING_INSTANCE_NAME)
		return err
	}
	if !(knservingInstance.Status.IsReady() == true) {
		//Return reconcile waiting for  knservingInstance ready
		e.logger.Info("KnativeServingInstance is not ready yet , Namespace: " + KNATIVE_SERVING_NAMESPACE + ", KnativeServerInstance :" + KNATIVE_SERVING_INSTANCE_NAME)
		return nil
	}

	return nil
}

func (e *external) createKnativeServingInstance(ctx context.Context) error {
	e.logger.Info("Creating KnativeServingInstanc ")

	knativeClient, err := knativeclient.NewForConfig(e.config)
	if err != nil {
		return nil
	}

	selector := `
        selector:
          sdlc.visibility: cluster-local
`
	knserving := &knativ1alpha1.KnativeServing{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: KNATIVE_SERVING_NAMESPACE,
			Name:      KNATIVE_SERVING_INSTANCE_NAME,
			Labels:    map[string]string{"ibm-aiops-install/install": "knative-serving"},
		},
		Spec: knativ1alpha1.KnativeServingSpec{
			CommonSpec: knativ1alpha1.CommonSpec{
				Config: knativ1alpha1.ConfigMapData{
					"autoscaler": map[string]string{
						"enable-scale-to-zero": "true",
					},
					"domain": map[string]string{
						"svc.cluster.local": selector,
					},
				},
			},
		},
	}

	knservingInstance, err := knativeClient.KnativeServings(KNATIVE_SERVING_NAMESPACE).Create(ctx, knserving, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {

		return err
	}

	e.logger.Info("Knative namespaces created :" + knservingInstance.Name)
	return nil
}

func (e *external) observeKnativeEventingInstance(ctx context.Context) error {
	component = DKnativeEveningInstance
	e.logger.Info("Observe KnativeEventingInstance existing for aiops ")

	knativeClient, err := knativeclient.NewForConfig(e.config)
	if err != nil {
		return nil
	}
	kneventingInstance, err := knativeClient.KnativeEventings(KNATIVE_EVENTING_NAMESPACE).Get(ctx, KNATIVE_EVENTING_INSTANCE_NAME, metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to list KnativeEventingInstance , Namespace: " + KNATIVE_EVENTING_NAMESPACE + ", KnativeEventingInstance :" + KNATIVE_EVENTING_INSTANCE_NAME)
		return err
	}
	if !(kneventingInstance.Status.IsReady() == true) {
		//Return reconcile waiting for  knservingInstance ready
		e.logger.Info("KnativeEventingInstance is not ready yet , Namespace: " + KNATIVE_EVENTING_NAMESPACE + ", KnativeEventingInstance :" + KNATIVE_EVENTING_INSTANCE_NAME)
		return nil
	}

	return nil
}

func (e *external) createKnativeEventingInstance(ctx context.Context) error {
	e.logger.Info("Creating KnativeEventingInstance ")

	knativeClient, err := knativeclient.NewForConfig(e.config)
	if err != nil {
		return nil
	}
	kneventing := &knativ1alpha1.KnativeEventing{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: KNATIVE_EVENTING_NAMESPACE,
			Name:      KNATIVE_EVENTING_INSTANCE_NAME,
			Labels:    map[string]string{"ibm-aiops-install/install": "knative-eventing"},
		},
	}

	kneventingInstance, err := knativeClient.KnativeEventings(KNATIVE_EVENTING_NAMESPACE).
		Create(ctx, kneventing, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("Knative namespaces created :" + kneventingInstance.Name)
	return nil
}

func (e *external) observeCatalogSources(ctx context.Context) error {
	component = CatalogSource
	e.logger.Info("Observe all CatalogSource existing for aiops ")

	//Check CatalogSources opencloud-operators
	opcs, err := e.opClient.OperatorsV1alpha1().
		CatalogSources(OCPMarketplaceNS).
		Get(ctx, "opencloud-operators", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to get CatalogSources , Namespace: " + OCPMarketplaceNS + ", name : opencloud-operators ")
		return err
	}
	if !(opcs.Status.GRPCConnectionState.LastObservedState == "READY") {
		//Return reconcile waiting for  CatalogSources opencloud-operators ready
		e.logger.Info("CatalogSources opencloud-operators is not ready yet , Namespace: " + OCPMarketplaceNS + ", name : opencloud-operators ")
		return nil
	}

	//Check CatalogSources ibm-operator-catalog
	opcs, err = e.opClient.OperatorsV1alpha1().
		CatalogSources(OCPMarketplaceNS).
		Get(ctx, "ibm-operator-catalog", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to get CatalogSources , Namespace: " + OCPMarketplaceNS + ", name : ibm-operator-catalog ")
		return err
	}
	if !(opcs.Status.GRPCConnectionState.LastObservedState == "READY") {
		//Return reconcile waiting for  CatalogSources ibm-operator-catalog ready
		e.logger.Info("CatalogSources ibm-operator-catalog is not ready yet , Namespace: " + OCPMarketplaceNS + ", name : ibm-operator-catalog ")
		return nil
	}

	//Check CatalogSources ibm-aiops-catalog
	opcs, err = e.opClient.OperatorsV1alpha1().
		CatalogSources(OCPMarketplaceNS).
		Get(ctx, "ibm-aiops-catalog", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to get CatalogSources , Namespace: " + OCPMarketplaceNS + ", name : ibm-aiops-catalog ")
		return err
	}
	if !(opcs.Status.GRPCConnectionState.LastObservedState == "READY") {
		//Return reconcile waiting for  CatalogSources ibm-aiops-catalog ready
		e.logger.Info("CatalogSources ibm-aiops-catalog is not ready yet , Namespace: " + OCPMarketplaceNS + ", name : ibm-aiops-catalog ")
		return nil
	}

	e.logger.Info("The aiops catalogsource is ready , build image is  " + opcs.Spec.Image)

	return nil
}

func (e *external) createCatalogSources(ctx context.Context, cr *v1alpha1.Cp4waiops) error {
	e.logger.Info("Creating all CatalogSources ")

	secrets := []string{DImagePullSecretName}
	catalogSource := &operatorv1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OCPMarketplaceNS,
			Name:      "opencloud-operators",
		},
		Spec: operatorv1alpha1.CatalogSourceSpec{
			DisplayName: "IBMCS Operators",
			Image:       "docker.io/ibmcom/ibm-common-service-catalog:latest",
			Publisher:   "IBM",
			SourceType:  "grpc",
			Secrets:     secrets,
		},
	}

	_, err := e.opClient.OperatorsV1alpha1().
		CatalogSources(OCPMarketplaceNS).
		Create(ctx, catalogSource, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	catalogSource = &operatorv1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OCPMarketplaceNS,
			Name:      "ibm-operator-catalog",
		},
		Spec: operatorv1alpha1.CatalogSourceSpec{
			DisplayName: "ibm-operator-catalog",
			Image:       "docker.io/ibmcom/ibm-operator-catalog:latest",
			Publisher:   "IBM",
			SourceType:  "grpc",
			Secrets:     secrets,
		},
	}

	_, err = e.opClient.OperatorsV1alpha1().
		CatalogSources(OCPMarketplaceNS).
		Create(ctx, catalogSource, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	catalogSource = &operatorv1alpha1.CatalogSource{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OCPMarketplaceNS,
			Name:      "ibm-aiops-catalog",
		},
		Spec: operatorv1alpha1.CatalogSourceSpec{
			DisplayName: "IBM AIOps Catalog",
			//	Image:       "icr.io/cpopen/aiops-orchestrator-catalog:3.1-latest",
			Image:      cr.Spec.ForProvider.Catalogsource.Image,
			Publisher:  "IBM",
			SourceType: "grpc",
			Secrets:    secrets,
		},
	}

	_, err = e.opClient.OperatorsV1alpha1().
		CatalogSources(OCPMarketplaceNS).
		Create(ctx, catalogSource, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("The aiops build image is  " + cr.Spec.ForProvider.Catalogsource.Image)

	e.logger.Info("All catalog resources are created ")
	return nil
}

func (e *external) observeAIOpsSubscription(ctx context.Context) error {
	component = AIOpsSubscription
	e.logger.Info("Observe AIOPS Subscription existing for aiops ")

	//Check CatalogSources opencloud-operators
	opaiops, err := e.opClient.OperatorsV1alpha1().
		Subscriptions(OCPOperatorNS).
		Get(ctx, AIOpsSubscriptionName, metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to get  Subscriptions, Namespace: " + OCPOperatorNS + ", name : " + AIOpsSubscriptionName)
		return err
	}
	if !(opaiops.Status.State == "AtLatestKnown") {
		//Return reconcile waiting for  CatalogSources opencloud-operators ready
		e.logger.Info("Subscriptions is not ready yet , Namespace: " + OCPOperatorNS + ", name : " + AIOpsSubscriptionName)
		return nil
	}
	return nil
}

func (e *external) createAIOpsSubscription(ctx context.Context, cr *v1alpha1.Cp4waiops) error {
	e.logger.Info("Creating all AIOPS subscription ")

	subscription := &operatorv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OCPOperatorNS,
			Name:      AIOpsSubscriptionName,
		},
		Spec: &operatorv1alpha1.SubscriptionSpec{
			Channel:                cr.Spec.ForProvider.Catalogsource.Channel,
			InstallPlanApproval:    operatorv1alpha1.ApprovalAutomatic,
			CatalogSource:          "ibm-aiops-catalog",
			CatalogSourceNamespace: OCPMarketplaceNS,
			Package:                "ibm-aiops-orchestrator",
		},
	}
	subscription, err := e.opClient.OperatorsV1alpha1().
		Subscriptions(OCPOperatorNS).
		Create(ctx, subscription, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("AIOPS subscription are created ")
	return nil
}

func (e *external) addSecret(auths *Auths, artifact string, auth []byte, email string) {
	sEnc := b64.StdEncoding.EncodeToString(auth)
	//e.logger.Info("Debug : auth is :" + sEnc)
	r := &Registry{}
	r.Auth = sEnc
	r.Email = email
	auths.Auths[artifact] = r
}

func (e *external) observeOCS(ctx context.Context) error {
	component = DOCS
	e.logger.Info("Observe openshift storage existing for aiops ")
	observed, err := e.getOCSYaml()
	if err != nil {
		return err
	}
	err = e.kubeclient.Get(ctx, types.NamespacedName{
		Namespace: observed.GetNamespace(),
		Name:      observed.GetName(),
	}, observed)
	if err != nil {
		e.logger.Info("Failed to get storageCluster , namespace: openshift-storage , name: ocs-storagecluster , " + err.Error())
		return err
	}
	return nil
}

func (e *external) createOCS(ctx context.Context) error {
	e.logger.Info("Creating all AIOPS subscription ")

	e.createOCSNS(ctx)
	e.createOCSGroup(ctx)
	e.createOCSSubcription(ctx)
	e.labelWorkers(ctx)
	e.createOCSLocalSubcription(ctx)
	e.createLocalStorage(ctx)
	e.createStorageCluster(ctx)
	return nil
}

func (e *external) createOCSNS(ctx context.Context) error {
	//create NS for ocs , add label
	namespaceSource := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   OCSNamespace,
			Labels: map[string]string{"openshift.io/cluster-monitoring": "true"},
		},
	}
	namespaceobj, err := e.kube.CoreV1().Namespaces().Create(context.TODO(), namespaceSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create namespace error , namespace : " + OCSNamespace)
		return err
	}
	e.logger.Info("namespace created " + namespaceobj.Name)

	//create NS for openshift local storage
	namespaceSource = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-local-storage",
		},
	}
	namespaceobj, err = e.kube.CoreV1().Namespaces().Create(context.TODO(), namespaceSource, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		e.logger.Info("create namespace error , namespace : openshift-local-storage")
		return err
	}
	e.logger.Info("namespace created " + namespaceobj.Name)
	return nil
}

func (e *external) createOCSGroup(ctx context.Context) error {

	//Create operator group
	opGroupsrc := &operatorv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OCSNamespace,
			Name:      "openshift-storage-operatorgroup",
		},
		Spec: operatorv1.OperatorGroupSpec{
			TargetNamespaces: []string{OCSNamespace},
		},
	}
	opGroup, err := e.opClient.OperatorsV1().
		OperatorGroups(OCSNamespace).
		Create(ctx, opGroupsrc, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("OCS Operator Group are created : " + opGroup.Name)

	//Create local operator group
	opGroupsrc = &operatorv1.OperatorGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "openshift-local-storage",
			Name:      "local-operator-group",
		},
		Spec: operatorv1.OperatorGroupSpec{
			TargetNamespaces: []string{"openshift-local-storage"},
		},
	}
	opGroup, err = e.opClient.OperatorsV1().
		OperatorGroups("openshift-local-storage").
		Create(ctx, opGroupsrc, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("OCS Operator Group are created : " + opGroup.Name)
	return nil
}

func (e *external) getDefaultChannel(ctx context.Context, manifestName string) (string, error) {
	e.logger.Info("Get manifest : " + manifestName)
	manifest := `{
		"apiVersion": "packages.operators.coreos.com/v1",
		"kind": "PackageManifest",
		"metadata": {
			"name": "` + manifestName + `",
			"namespace": "default"
			}
		}`

	desired, err := getDesired(manifest)
	if err != nil {
		e.logger.Info("error unmarsha json , " + err.Error())
		return "", err
	}
	observed := desired.DeepCopy()
	err = e.kubeclient.Get(ctx, types.NamespacedName{
		Name:      observed.GetName(),
		Namespace: observed.GetNamespace(),
	}, observed)
	if err != nil {
		e.logger.Info("Failed to get PackageManifest , name: " + observed.GetName() + " , " + err.Error())
		return "", err
	}
	defaultchannel, find, err := unstructured.NestedString(observed.Object, "status", "defaultChannel")
	if err != nil || !find {
		e.logger.Info("Failed to get PackageManifest , name: " + observed.GetName() + " , status.defaultChannel ")
		return "", err
	}

	e.logger.Info("The PackageManifest , name: " + observed.GetName() + " , defaultChannel: " + defaultchannel)
	return defaultchannel, nil
}

func (e *external) createOCSSubcription(ctx context.Context) error {

	//Get target openshift default channel name
	defaultchannel, err := e.getDefaultChannel(ctx, "ocs-operator")
	if err != nil {
		return err
	}
	subscription := &operatorv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: OCSNamespace,
			Name:      "ocs-operator",
		},
		Spec: &operatorv1alpha1.SubscriptionSpec{
			// TODO: won't speicif the channel version , use default one in target ocp
			Channel:                defaultchannel,
			InstallPlanApproval:    operatorv1alpha1.ApprovalAutomatic,
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: OCPMarketplaceNS,
			Package:                "ocs-operator",
		},
	}
	subscription, err = e.opClient.OperatorsV1alpha1().
		Subscriptions(OCSNamespace).
		Create(ctx, subscription, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("OCS subscription are created ")

	return nil
}

func (e *external) labelWorkers(ctx context.Context) error {
	//create NS for ocs , add label

	nodeList, err := e.kube.CoreV1().Nodes().List(
		context.TODO(),
		metav1.ListOptions{
			LabelSelector: "node-role.kubernetes.io/worker",
		},
	)
	if err != nil && nodeList == nil {
		e.logger.Info("error fetch matched nodes")
		return err
	}

	for _, node := range nodeList.Items {
		labels := node.ObjectMeta.Labels
		labels["cluster.ocs.openshift.io/openshift-storage"] = ""
		node.ObjectMeta.Labels = labels
		nodeModify, err := e.kube.CoreV1().Nodes().Update(context.TODO(), &node, metav1.UpdateOptions{})
		if err != nil {
			e.logger.Info("ERROR , Update Node " + nodeModify.Name + "label failed ")
			return err
		}
	}
	return nil
}

func (e *external) createOCSLocalSubcription(ctx context.Context) error {

	//Get target openshift default channel name
	defaultchannel, err := e.getDefaultChannel(ctx, "local-storage-operator")
	if err != nil {
		return err
	}
	subscription := &operatorv1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "openshift-local-storage",
			Name:      "local-storage-operator",
		},
		Spec: &operatorv1alpha1.SubscriptionSpec{
			// TODO: won't speicif the channel version , use default one in target ocp
			Channel:                defaultchannel,
			InstallPlanApproval:    operatorv1alpha1.ApprovalAutomatic,
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: OCPMarketplaceNS,
			Package:                "local-storage-operator",
		},
	}
	subscription, err = e.opClient.OperatorsV1alpha1().
		Subscriptions("openshift-local-storage").
		Create(ctx, subscription, metav1.CreateOptions{})

	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	e.logger.Info("OCS local storage operator subscription are created ")

	return nil
}

func (e *external) createLocalStorage(ctx context.Context) error {
	e.logger.Info("Observe local storage Subscription existing for OCS ")

	//Check CatalogSources opencloud-operators
	opaiops, err := e.opClient.OperatorsV1alpha1().
		Subscriptions("openshift-local-storage").
		Get(ctx, "local-storage-operator", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to get  Subscriptions, Namespace: openshift-local-storage , name : local-storage-operator ")
		return err
	}
	if !(opaiops.Status.State == "AtLatestKnown") {
		//Return reconcile waiting for  CatalogSources opencloud-operators ready
		e.logger.Info("Subscriptions is not ready yet , Namespace: openshift-local-storage , name : local-storage-operator")
		return errors.New("the subscription is not ready")
	}
	e.logger.Info("start creating localvolume")
	localDeviceName := "/dev/vdb"
	localVolumeJson := `{
		"apiVersion": "local.storage.openshift.io/v1",
		"kind": "LocalVolume",
		"metadata": {
			"name": "local-disks",
			"namespace": "openshift-local-storage"
		},
		"spec": {
			"nodeSelector": {
				"nodeSelectorTerms": [
					{
						"matchExpressions": [
							{
								"key": "cluster.ocs.openshift.io/openshift-storage",
								"operator": "In",
								"values": [
									""
								]
							}
						]
					}
				]
			},
			"storageClassDevices": [
				{
					"devicePaths": [
						"` + localDeviceName + `"
					],
					"storageClassName": "localblock-sc",
					"volumeMode": "Block"
				}
			]
		}
	}`
	desired, err := getDesired(localVolumeJson)
	if err != nil {
		e.logger.Info("error unmarsha json")
		e.logger.Info(err.Error())
		return err
	}
	observed := desired.DeepCopy()
	err = e.kubeclient.Create(ctx, observed)
	if err != nil {
		e.logger.Info("Create localvolume failed , namespace: openshift-local-storage , name: local-disks ")
		e.logger.Info(err.Error())
		return err
	}
	e.logger.Info("Create localvolume complete , namespace: openshift-local-storage , name: local-disks ")
	return nil
}

func (e *external) getOCSYaml() (*unstructured.Unstructured, error) {
	RAW_DISK_SIZE := "200"
	DEVICE_SET_COUNT := "2"
	storageCluster := `{
		"apiVersion": "ocs.openshift.io/v1",
		"kind": "StorageCluster",
		"metadata": {
			"name": "ocs-storagecluster",
			"namespace": "openshift-storage"
		},
		"spec": {
			"manageNodes": false,
			"monDataDirHostPath": "/var/lib/rook",
			"resources": {
				"mds": {
					"limits": {
						"cpu": "3",
						"memory": "9Gi"
					},
					"requests": {
						"cpu": "100m",
						"memory": "512Mi"
					}
				}
			},
			"storageDeviceSets": [
				{
					"count": ` + DEVICE_SET_COUNT + `,
					"dataPVCTemplate": {
						"spec": {
							"accessModes": [
								"ReadWriteOnce"
							],
							"resources": {
								"requests": {
									"storage": "` + RAW_DISK_SIZE + `Gi"
								}
							},
							"storageClassName": "localblock-sc",
							"volumeMode": "Block"
						}
					},
					"name": "ocs-deviceset",
					"placement": {},
					"portable": false,
					"replica": 3,
					"resources": {
						"limits": {
							"cpu": "2",
							"memory": "10Gi"
						},
						"requests": {
							"cpu": "100m",
							"memory": "512Mi"
						}
					}
				}
			]
		}
	}`
	desired, err := getDesired(storageCluster)
	if err != nil {
		e.logger.Info("error unmarsha json , " + err.Error())
		return nil, err
	}
	observed := desired.DeepCopy()
	return observed, nil
}

func (e *external) createStorageCluster(ctx context.Context) error {
	e.logger.Info("Observe ocs operator Subscription existing for OCS ")

	//Check CatalogSources opencloud-operators
	opaiops, err := e.opClient.OperatorsV1alpha1().
		Subscriptions("openshift-storage").
		Get(ctx, "ocs-operator", metav1.GetOptions{})
	if err != nil {
		e.logger.Info("Not able to get  Subscriptions, Namespace: openshift-storage , name : ocs-operator ")
		return err
	}
	if !(opaiops.Status.State == "AtLatestKnown") {
		//Return reconcile waiting for  CatalogSources opencloud-operators ready
		e.logger.Info("Subscriptions is not ready yet , Namespace: openshift-storage , name : ocs-operator")
		return errors.New("the subscription is not ready")
	}
	e.logger.Info("start creating Storage Cluster")
	observed, err := e.getOCSYaml()
	if err != nil {
		return err
	}
	err = e.kubeclient.Create(ctx, observed)
	if err != nil {
		e.logger.Info("Create storageCluster failed , namespace: openshift-storage , name: ocs-storagecluster , " + err.Error())
		return err
	}
	e.logger.Info("Create localvolume complete , namespace: openshift-storage , name: ocs-storagecluster ")
	return nil
}

func getDesired(s string) (*unstructured.Unstructured, error) {
	desired := &unstructured.Unstructured{}
	if err := json.Unmarshal([]byte(s), desired); err != nil {
		return nil, errors.Wrap(err, errUnmarshalTemplate)
	}
	return desired, nil
}

func (e *external) observeCP4WAIOPS(ctx context.Context, cr *v1alpha1.Cp4waiops) error {
	component = DWAIOPS
	e.logger.Info("Observe CP4WAIOPS installation")
	installationsrc := `{
		"apiVersion": "orchestrator.aiops.ibm.com/v1alpha1",
		"kind": "Installation",
		"metadata": {
			"name": "ibm-cp-watson-aiops",
			"namespace": "` + cr.Spec.ForProvider.InstallParams.Namespace + `"
			}
		}`

	desired, err := getDesired(installationsrc)
	if err != nil {
		e.logger.Info("error unmarsha json , " + err.Error())
		return err
	}
	observed := desired.DeepCopy()
	err = e.kubeclient.Get(ctx, types.NamespacedName{
		Namespace: observed.GetNamespace(),
		Name:      observed.GetName(),
	}, observed)
	if err != nil {
		e.logger.Info("Failed to get CP4WAIOPS , namespace: " + observed.GetNamespace() + " , name: " + observed.GetName() + " , " + err.Error())
		return err
	}
	return nil
}

func (e *external) getWAIOPSYaml(cr *v1alpha1.Cp4waiops) (*unstructured.Unstructured, error) {

	var modules bytes.Buffer
	size := len(cr.Spec.ForProvider.InstallParams.PakModules)
	if size > 0 {
		for num, module := range cr.Spec.ForProvider.InstallParams.PakModules {
			modules.WriteString("{\"name\":\"" + module.Name + "\",")
			modules.WriteString("\"enabled\":" + strconv.FormatBool(module.Enabled))
			// If there's pakmodule config
			configs := module.Configs
			if configs != nil {
				modules.WriteString(",\"config\":[")
				conSize := len(configs)
				for nconfig, config := range configs {
					modules.WriteString("{\"name\":\"" + config.Name + "\",")
					moduleSpec := config.Spec
					modules.WriteString("\"spec\": ")
					data, err := json.Marshal(moduleSpec)
					if err != nil {
						e.logger.Info("failed to marsha module config spec !!!")
					}
					e.logger.Info("the module config spec , " + string(data))
					modules.WriteString(string(data))
					modules.WriteString("}")
					if nconfig < conSize-1 {
						modules.WriteString(",")
					}
					modules.WriteString("]")
				}
			}
			modules.WriteString("}")
			if num < size-1 {
				modules.WriteString(",")
			}
		}
	}

	installationsrc := `{
		"apiVersion": "orchestrator.aiops.ibm.com/v1alpha1",
		"kind": "Installation",
		"metadata": {
			"name": "ibm-cp-watson-aiops",
			"namespace": "` + cr.Spec.ForProvider.InstallParams.Namespace + `"
		},
		"spec": {
			"imagePullSecret": "` + DImagePullSecretName + `" ,
			"license": {
				"accept": ` + strconv.FormatBool(cr.Spec.ForProvider.InstallParams.License.Accept) + `
			},
			"pakModules": [
				` + modules.String() + `
			],
			"size": "` + cr.Spec.ForProvider.InstallParams.Size + `",
			"storageClass": "` + cr.Spec.ForProvider.InstallParams.StorageClass + `",
			"storageClassLargeBlock": "` + cr.Spec.ForProvider.InstallParams.StorageClassLargeBlock + `"
		}
	}`
	e.logger.Info("the installation yaml , " + installationsrc)
	desired, err := getDesired(installationsrc)
	if err != nil {
		e.logger.Info("error unmarsha json , " + err.Error())
		return nil, err
	}
	observed := desired.DeepCopy()
	return observed, nil
}

func (e *external) createCP4WAIOPS(ctx context.Context, cr *v1alpha1.Cp4waiops) error {
	e.logger.Info("Create cp4waiops installation  ")

	observed, err := e.getWAIOPSYaml(cr)
	if err != nil {
		return err
	}
	err = e.kubeclient.Create(ctx, observed)
	if err != nil {
		e.logger.Info("Create CP4WAIOPS installation failed " + err.Error())
		return err
	}
	e.logger.Info("Create CP4WAIOPS installation successfully ")
	return nil
}

func (e *external) validateCP4WAIOPS(ctx context.Context) error {
	component = Validate
	e.logger.Info("Velidate CP4WAIOPS installation")

	e.logger.Info("Checking if pods pullimage error")
	e.fixPodImagePullError(ctx, OCPOperatorNS)
	e.fixPodImagePullError(ctx, "ibm-common-services")
	e.fixPodImagePullError(ctx, aiopsNamespace)

	return nil
}

func (e *external) fixPodImagePullError(ctx context.Context, namespace string) error {
	podList, err := e.kube.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "status.phase=Pending",
	})
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil && cs.State.Waiting.Reason == "ImagePullBackOff" {
				e.logger.Info("Pod " + pod.Name + " in namespace " + namespace + " failed to pull image , restart it")
				err := e.kube.CoreV1().Pods(namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
				if err != nil {
					e.logger.Info("ERROR , Delete Pod " + pod.Name + " failed ")
					return err
				}
				e.logger.Info("Pod " + pod.Name + " in namespace " + namespace + " restarted")
			}
		}
	}
	return nil
}
