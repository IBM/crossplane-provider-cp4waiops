<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->
**Table of Contents**  *generated with [DocToc](https://github.com/thlorenz/doctoc)*

- [Crossplane Provider for Cloud Pak for Watson AIOps](#crossplane-provider-for-cloud-pak-for-watson-aiops)
  - [Prerequisites](#prerequisites)
  - [Tutorial](#tutorial)
    - [Clone The Repo](#clone-the-repo)
    - [Create Secret with the Kubernetes Kubeconfig](#create-secret-with-the-kubernetes-kubeconfig)
    - [Create Secret with IBM entitlement key](#create-secret-with-ibm-entitlement-key)
    - [Deploy Cloudpak Provider](#deploy-cloudpak-provider)
    - [Create Provider Config](#create-provider-config)
    - [Config Storageclass](#config-storageclass)
    - [Install Cloud Pak for Watson AIOps](#install-cloud-pak-for-watson-aiops)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

# Crossplane Provider for Cloud Pak for Watson AIOps

`crossplane-provider-cp4waiops` is a [Crossplane](https://crossplane.io/) Provider, which helps preparing dependencies and installing cp4waiops, the goal is to automate installing procedure, by observing each steps status in target OCP cluster. If not yet applied, it then helps executing and abservinig the current step until completion .

## Prerequisites

- A Kubernetes cluster https://kubernetes.io/
- Install crossplane https://crossplane.io in above Kubernetes cluster
- A target OpenShift cluster for Cloud Pak for Wastson AIOps

## Tutorial

### Clone The Repo

```shell
git clone https://github.com/cloud-pak-gitops/crossplane-provider-cp4waiops.git
cd crossplane-provider-cp4waiops
```

### Create Secret with the Kubernetes Kubeconfig

Using the kubeconfig in this repo as examplea:

```shell
kubectl create secret generic openshift-cluster-kubeconfig --from-file=credentials=./examples/provider/kubeconfig -n crossplane-system
```

**Note:** You need to replace the sample kubeconfig with your target kubeconfig

### Create Secret with IBM entitlement key

```
kubectl create secret generic image-pull-secret --from-literal=cp.icr.io=cp:<entitlement-key> -n crossplane-system
```

**Note:** refer to [CP4WAIOPS-KC](https://www.ibm.com/docs/en/cloud-paks/cp-waiops/3.1.0?topic=installing-preparing-install-cloud-pak#entitlement_keys) to replace the `entitlement-key` 

### Deploy Cloudpak Provider

```shell
kubectl apply -f package/crds/ -R
kubectl apply -f cluster/deploy -R
```

and verify the pod crossplane-provider-cloudpak is running 

```shell
kubectl get po -n crossplane-system
```

The output would be as following:
```console
$ kubectl get po -n crossplane-system
NAME                                    READY   UP-TO-DATE   AVAILABLE   AGE
crossplane                              1/1     1            1           21d
crossplane-provider-cloudpak            1/1     1            1           17h
crossplane-rbac-manager                 1/1     1            1           21d
```

### Create Provider Config

The providerConfig will tell the provider which OpenShift will be connected and deploy the Cloud Pak for Waston AIOps.


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

The storageclass is not handled in this repo yet, since there're multiple ways for different infrastrctures.

If using ROKS, you may create Portworx following [official doc](https://www.ibm.com/docs/en/cloud-paks/cp-waiops/3.1.0?topic=requirements-storage-considerations#portworx-cli) 

**Coming soon:**   
We have tried to prepare OCS(local-disk) successfully , will separate it as an individual CR for user to consume , before that you still need to prepare storage yourself .

### Install Cloud Pak for Watson AIOps

We need to create the CP4WAIOPS CR to install the Cloud Pak for Watson AIOps to target OpenShit Cluster.

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

**Note:** You may want to modify the values in yaml to customize your installation.



