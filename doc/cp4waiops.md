# Deploy CP4WAIOPS via crossplane



## Prerequisites
- Crossplane server , https://crossplane.io
- A target openshift cluster , install cp4waiops
  


## Install CP4WAIOPS via crossplane


![w](../images/instana-on-kind-via-crossplane.png)



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
crossplane-provider-helm-f238b9a54ac7   1/1     1            1           21d
crossplane-rbac-manager                 1/1     1            1           21d
```


### Create provider config , telling provider which OCP will be installed cloudpak


```shell
kubectl apply -f examples/provider/config.yaml
```

### Create a Dependency CR to prepare dependencies in target cluster 

```shell
kubectl apply -f examples/dependency/dependency.yaml
```



### About Storageclass 

The storageclass is not handled in this repo yet , since there're multiple ways for different infrastrctures .  
If using fyre , you can leverage [scripts](https://github.ibm.com/jbjhuang/rook-ceph/blob/master/setup-rook-v1.5.8.sh) to create ceph .  
If using AWS , I tried to use gp2 and Portworx , unfortunately it doesn't work , but you're able to see the installing process though
If using ROKS , you may create Portworx following [official doc](https://www.ibm.com/docs/en/cloud-paks/cp-waiops/3.1.0?topic=requirements-storage-considerations#portworx-cli) 

TODO: consider to provide virious Storageclass preparing ?

### Install aiops ( Only expose limited params for customer)

```shell
kubectl apply -f examples/aiops/installation.yaml 
```

**Note:** You need modify the values in yaml to customize your installation 




