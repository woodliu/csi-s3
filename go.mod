module github.com/woodliu/csi-s3

require (
	github.com/container-storage-interface/spec v1.2.0
	github.com/ctrox/csi-s3 v1.1.1
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/kahing/goofys v0.19.0
	github.com/kubernetes-csi/csi-test v2.0.0+incompatible
	github.com/kubernetes-csi/drivers v1.0.2
	github.com/minio/minio-go v0.0.0-20190430232750-10b3660b8f09
	github.com/mitchellh/go-ps v0.0.0-20170309133038-4fdf99ab2936
	github.com/onsi/ginkgo v1.5.0
	github.com/onsi/gomega v1.4.0
	golang.org/x/net v0.0.0-20190213061140-3a22650c66bd
	google.golang.org/grpc v1.13.0
	k8s.io/kubernetes v1.13.4
	launchpad.net/gocheck v0.0.0-20140225173054-000000000087 // indirect
)

go 1.13
