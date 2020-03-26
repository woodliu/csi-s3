/*
Copyright 2017 The Kubernetes Authors.

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

package s3

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/golang/glog"
	obs "github.com/woodliu/csi-s3/pkg/hw-obs"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi/v0"
	"github.com/kubernetes-csi/drivers/pkg/csi-common"
)

type controllerServer struct {
	*csicommon.DefaultControllerServer
}

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	// volumeID is bucket name
	volumeID := sanitizeVolumeID(req.GetName())

	if err := cs.Driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		glog.V(3).Infof("invalid create volume req: %v", req)
		return nil, err
	}

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}

	capacityBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	/* get para from StorageClass.parameters
	params := req.GetParameters()
	mounter := params[mounterTypeKey]
	*/
	glog.V(4).Infof("Got a request to create volume %s", volumeID)
	glog.V(4).Infof("Got a request to create AccessKeyID %s SecretAccessKey %s Endpoint %s", req.ControllerCreateSecrets["accessKeyID"], req.ControllerCreateSecrets["secretAccessKey"], req.ControllerCreateSecrets["endpoint"])

	// Changed:use hw SDK
	obsClient, err := newObsClientFromSecrets(req.GetControllerCreateSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Obs client: %s", err)
	}

	_, err = obsClient.HeadBucket(volumeID)
	if err == nil {
		// bucket exist
		output, err := obsClient.GetBucketStorageInfo(volumeID)
		if obsError, ok := err.(obs.ObsError); ok {
			return nil, fmt.Errorf("failed to get bucket capacity of bucket %s, Code:%s, Message:%s", volumeID, obsError.Message)
		}
		// Check if volume capacity requested is bigger than the already existing capacity
		if capacityBytes > output.Size {
			return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("Volume with the same name: %s but with smaller size already exist", volumeID))
		}

	} else if obsError, ok := err.(obs.ObsError); ok {
		if obsError.StatusCode == 404 {
			// bucket not exist
			input := &obs.CreateBucketInput{}
			input.Bucket = volumeID
			// Create bucket
			_, err := obsClient.CreateBucket(input)
			if obsError, ok := err.(obs.ObsError); ok {
				return nil, fmt.Errorf("failed to create volume %s: Code:%s, Message:%s", volumeID, obsError.Code,obsError.Message)
			}

			objInput := &obs.PutObjectInput{}
			objInput.Bucket = volumeID
			objInput.Key = fsPrefix + "/"
			objInput.Body = strings.NewReader("")
			_, err = obsClient.PutObject(objInput)
			if obsError, ok := err.(obs.ObsError); ok {
				return nil, fmt.Errorf("failed to create prefix %s: Code:%s, Message:%s", fsPrefix, obsError.Code,obsError.Message)
			}
		} else {
			return nil, fmt.Errorf("StatusCode:%d",obsError.StatusCode)
		}
	}


	/* Replace original code
	exists, err := s3.bucketExists(volumeID)
	if err != nil {
		return nil, fmt.Errorf("failed to check if bucket %s exists: %v", volumeID, err)
	}
	if exists {
		var b *bucket
		b, err = s3.getBucket(volumeID)
		if err != nil {
			return nil, fmt.Errorf("failed to get bucket metadata of bucket %s: %v", volumeID, err)
		}
		// Check if volume capacity requested is bigger than the already existing capacity
		if capacityBytes > b.CapacityBytes {
			return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("Volume with the same name: %s but with smaller size already exist", volumeID))
		}
	} else {
		if err = s3.createBucket(volumeID); err != nil {
			return nil, fmt.Errorf("failed to create volume %s: %v", volumeID, err)
		}
		if err = s3.createPrefix(volumeID, fsPrefix); err != nil {
			return nil, fmt.Errorf("failed to create prefix %s: %v", fsPrefix, err)
		}
	}
	*/

	// Set capacity
	input := &obs.SetBucketQuotaInput{}
	input.Bucket = volumeID

	input.Quota = capacityBytes
	_, err = obsClient.SetBucketQuota(input)
	if obsError, ok := err.(obs.ObsError); ok {
		return nil, fmt.Errorf("Error setting bucket capacityBytes: %v, Code:%s, Message:%s", err, obsError.Code,obsError.Message)
	}

	//TODO：need process mounter
	/*
	b := &bucket{
		Name:          volumeID,
		Mounter:       mounter,
		CapacityBytes: capacityBytes,
		FSPath:        fsPrefix,
	}
	if err := s3.setBucket(b); err != nil {
		return nil, fmt.Errorf("Error setting bucket metadata: %v", err)
	}
*/
	glog.V(4).Infof("create volume %s", volumeID)
	s3Vol := s3Volume{}
	s3Vol.VolName = volumeID
	s3Vol.VolID = volumeID
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			Id:      volumeID,
			CapacityBytes: capacityBytes,
			Attributes: req.GetParameters(),
		},
	}, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	volumeID := req.GetVolumeId()

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	if err := cs.Driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		glog.V(3).Infof("Invalid delete volume req: %v", req)
		return nil, err
	}
	glog.V(4).Infof("Deleting volume %s", volumeID)

	/*
	s3, err := newS3ClientFromSecrets(req.GetControllerDeleteSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %s", err)
	}
    */
	// Changed:use hw SDK
	obsClient, err := newObsClientFromSecrets(req.GetControllerDeleteSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Obs client: %s", err)
	}

	_, err = obsClient.HeadBucket(volumeID)
	if obsError, ok := err.(obs.ObsError); ok {
		if obsError.StatusCode == 404 {
			// bucket not exist
			glog.V(5).Infof("Bucket %s does not exist, ignoring request", volumeID)
		} else {
			glog.V(3).Infof("Failed to remove volume %s: %v", volumeID, err)
			return nil, err
		}
	}else {
		// bucket exist
		if err := emptyObsBucket(obsClient, volumeID); err != nil {
			glog.V(3).Infof("Failed to remove volume %s: %v", volumeID, err)
			return nil, err
		}

		_, err := obsClient.DeleteBucket(volumeID)
		if obsError, ok := err.(obs.ObsError); ok {
			glog.V(3).Infof("Failed to remove volume %s: %s", volumeID, obsError.Message)
			return nil, err
		}
	}

	/*
	s3, err := newS3ClientFromSecrets(req.GetSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %s", err)
	}
	exists, err := s3.bucketExists(volumeID)
	if err != nil {
		return nil, err
	}
	if exists {
		if err := s3.removeBucket(volumeID); err != nil {
			glog.V(3).Infof("Failed to remove volume %s: %v", volumeID, err)
			return nil, err
		}
	} else {
		glog.V(5).Infof("Bucket %s does not exist, ignoring request", volumeID)
	}
	*/

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities missing in request")
	}

	/*
	s3, err := newS3ClientFromSecrets(req.GetSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %s", err)
	}

	var obsClient, _ = obs.New(s3.cfg.AccessKeyID, s3.cfg.SecretAccessKey, s3.cfg.Endpoint)
	_, err = obsClient.HeadBucket(req.GetVolumeId())
	if obsError, ok := err.(obs.ObsError); ok {
		if obsError.StatusCode == 404 {
			// bucket not exist
			return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume with id %s does not exist", req.GetVolumeId()))
		} else {
			return nil, err
		}
	}

	*/

	/*
	exists, err := s3.bucketExists(req.GetVolumeId())
	if err != nil {
		return nil, err
	}
	if !exists {
		// return an error if the volume requested does not exist
		return nil, status.Error(codes.NotFound, fmt.Sprintf("Volume with id %s does not exist", req.GetVolumeId()))
	}
    */
	// We currently only support RWO
	supportedAccessMode := &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
	}

	for _, cap := range req.VolumeCapabilities {
		if cap.GetAccessMode().GetMode() != supportedAccessMode.GetMode() {
			return &csi.ValidateVolumeCapabilitiesResponse{Supported:false, Message: "Only single node writer is supported"}, nil
		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{Supported: true}, nil
}


func sanitizeVolumeID(volumeID string) string {
	volumeID = strings.ToLower(volumeID)
	if len(volumeID) > 63 {
		h := sha1.New()
		io.WriteString(h, volumeID)
		volumeID = hex.EncodeToString(h.Sum(nil))
	}
	return volumeID
}

func emptyObsBucket(obsClient *obs.ObsClient, volumeId string) error{

	removeAllFlag := true

	input := &obs.ListObjectsInput{}
	input.Bucket = volumeId
	output, listErr := obsClient.ListObjects(input)
	if listErr == nil {
		input := &obs.DeleteObjectInput{}
		input.Bucket = volumeId
		for _, val := range output.Contents {
			input.Key = val.Key
			_, err := obsClient.DeleteObject(input)
			if obsError, ok := err.(obs.ObsError); ok {
				glog.Errorf("Failed to remove object %s, error: %s", val.Key, obsError.Message)
				removeAllFlag = false
			}
		}
	}

	if listErr != nil {
		glog.Error("Error listing objects", listErr)
		return listErr
	}


	if removeAllFlag != true {
		return fmt.Errorf("Failed to remove all objects of bucket %s", volumeId)
	}

	_,errPrefix := obsClient.DeleteObject(&obs.DeleteObjectInput{volumeId,fsPrefix,""})
	// ensure our prefix is also removed
	return errPrefix
}
