MooseFs CSI
===========

CSI driver backed by MooseFs

[MooseFs](https://moosefs.com) is an open-source distributed file system that can run on commodity hardware or cloud infrastructure.

Kubernetes installation
-----------------------

Kubernetes manifests are provided for deployment using [kustomize](https://kustomize.io) or the built-in `kubectl apply -k`.

### Configuration and Versioning

In [kustomization.yaml](kubernetes/kustomization.yaml), there are three configurable entries for setting up the connection to the storage cluster and the plugin version to install.

#### Config

At the bottom of the file are the moosefs connection parameters:

-   MFS_SERVER as a hostname `"mfsmaster"` or a host:port `"mfsmaster:9421"`

-   MFS_ROOT_PATH is the root directory for the CSI-managed space.  
    This should match a moosefs export, to facilitate management of the alloted space, storage class, trash time, and permissions.

#### Version

MooseFs CSI is versioned to indicate the mfsmount client and the plugin, using the pattern of `<vMooseFs>-<vPlugin>`.  
The latest version of MooseFs is `v3.0.115` (this should match the version running on your MooseFs cluster).  
The latest release of moosefs-csi is `v0.1.0`.  
These two versions are in the docker image at `kunde21/moosefs-csi:v3.0.115-v0.1.0`.

Deploy
------

Configure the connection to your MooseFs cluster and deploy:

```sh
$ git clone https://github.com/Kunde21/moosefs-csi
$ cd moosefs-csi/kubernetes

<edit kustomization.yaml>

$ kubectl -n kube-system apply -k .
# or
$ kustomize build | kubectl -n kube-system apply -f -
```

## Examples

Check the [examples](kubernetes/examples) directory. 
