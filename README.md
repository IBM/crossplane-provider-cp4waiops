<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [crossplane-provider-cp4waiops](#crossplane-provider-cp4waiops)
  - [Prerequisites](#prerequisites)
  - [Tutorial](#tutorial)
    - [Clone the repo](#clone-the-repo)
    - [Create secret storing the kubeconfig](#create-secret-storing-the-kubeconfig)
    - [Create a secret storing your entitlement key:](#create-a-secret-storing-your-entitlement-key)
    - [Deploy the cloudpak provider](#deploy-the-cloudpak-provider)
    - [Create provider config](#create-provider-config)
    - [Config Storageclass](#config-storageclass)
    - [Create a cp4waiops CR to install CP4WAIOPS in target cluster](#create-a-cp4waiops-cr-to-install-cp4waiops-in-target-cluster)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# crossplane-provider-cp4waiops

`crossplane-provider-cp4waiops` is a [Crossplane](https://crossplane.io/) Provider 
, which helps preparing dependencies and installing cp4waiops , the goal is to automate installing procedure , by observing  each steps status in target OCP cluster , if not yet applied , then it helps executing and abservinig the current step until completion .

## Prerequisites

- You need install a kubernetes cluster https://kubernetes.io/
- You need install crossplane https://crossplane.io in above kubernetes cluster
- You need a target openshift cluster , will install cloudpak

## Tutorial

Following executing are all in cluster which installing crossplane 

### Clone the repo


```shell
git clone https://github.com/IBM/crossplane-provider-cp4waiops.git
cd crossplane-provider-cp4waiops
```

### Create secret storing the kubeconfig 

Using the kubeconfig in this repo as example :

```shell
kubectl create secret generic openshift-cluster-kubeconfig --from-file=credentials=./examples/provider/kubeconfig -n crossplane-system
```

**Note:** You need replace the sample kubeconfig with your target kubeconfig 

### Create a secret storing your entitlement key:

```
kubectl create secret generic image-pull-secret --from-literal=cp.icr.io=cp:<entitlement-key> -n crossplane-system
```

**Note:** refer to [CP4WAIOPS-KC](https://www.ibm.com/docs/en/cloud-paks/cp-waiops/3.1.0?topic=installing-preparing-install-cloud-pak#entitlement_keys) to replace the `entitlement-key` 



### Deploy the cloudpak provider 

```shell
kubectl apply -f package/crds/ -R
kubectl apply -f cluster/deploy -R
```

and verify the pod crossplane-provider-cloudpak is running 

```shell
kubectl get po -n crossplane-system
NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
crossplane                              1/1     1            1           21d
crossplane-provider-cloudpak            1/1     1            1           17h
crossplane-rbac-manager                 1/1     1            1           21d
```


### Create provider config

The providerConfig will tell the provider which OCP will be connected and deploy cloudpak.


```shell
cat << EOF | oc apply -f -
apiVersion: cloudpak.crossplane.io/v1alpha1
kind: ProviderConfig
metadata:
  name: openshift-cluster-provider-config 
spec:
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: openshift-cluster-kubeconfig 
      key: credentials
EOF
```

### Config Storageclass

The storageclass is not handled in this repo yet , since there're multiple ways for different infrastrctures .  
If using ROKS , you may create Portworx following [official doc](https://www.ibm.com/docs/en/cloud-paks/cp-waiops/3.1.0?topic=requirements-storage-considerations#portworx-cli) 

**Coming soon:**   
We have tried to prepare OCS(local-disk) successfully , will separate it as an individual CR for user to consume , before that you still need to prepare storage yourself .


### Create a cp4waiops CR to install CP4WAIOPS in target cluster 

```shell
cat << EOF | oc apply -f -
apiVersion: cloudpak.crossplane.io/v1alpha1
kind: Cp4waiops
metadata:
  name: cp4waiops
spec:
  forProvider:
    catalogsource:
      image: icr.io/cpopen/aiops-orchestrator-catalog:3.1-latest
      channel: v3.1
    installParams:
      imagePullSecret: ibm-entitlement-key
      namespace: cp4waiops
      license: 
        accept: true
      pakModules:
        - name: aiManager
          enabled: true
        - name: aiopsFoundation
          enabled: true
        - name: applicationManager
          enabled: true
          config: 
          - name: noi-operator
            spec: 
              noi:
                persistence:
                  storageClassDB2: rook-cephfs
      size: small
      storageClass: rook-cephfs
      storageClassLargeBlock: rook-cephfs
  providerConfigRef:
    name: openshift-cluster-provider-config 
EOF
```

**Note:** You need modify the values in yaml to customize your installation 



