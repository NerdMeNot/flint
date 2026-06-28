package runner

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// rwoDenyList lists provisioners that definitely do NOT support ReadWriteMany —
// hard-rejected with a clear message pointing at the RWX alternative.
var rwoDenyList = map[string]string{
	"ebs.csi.aws.com":          "AWS EBS (gp2/gp3/io2) is ReadWriteOnce only — use efs.csi.aws.com for shared workspaces",
	"disk.csi.azure.com":       "Azure Managed Disks are ReadWriteOnce only — use file.csi.azure.com for shared workspaces",
	"pd.csi.storage.gke.io":    "GCP Persistent Disk is ReadWriteOnce only — use filestore.csi.storage.gke.io for shared workspaces",
	"kubernetes.io/aws-ebs":    "AWS EBS (legacy in-tree) is ReadWriteOnce only — use efs.csi.aws.com",
	"kubernetes.io/gce-pd":     "GCP PD (legacy in-tree) is ReadWriteOnce only — use filestore.csi.storage.gke.io",
	"kubernetes.io/azure-disk": "Azure Disk (legacy in-tree) is ReadWriteOnce only — use file.csi.azure.com",
}

// ValidateStorageClassRWX checks that a workspace=pvc pool's StorageClass can
// support ReadWriteMany. Needs a Kubernetes client (the worker has one; the API
// server doesn't, so the API does only structural validation). Known-RWO
// provisioners are rejected; known-RWX pass; unknown are allowed (return nil) on
// the assumption a custom CSI driver may support RWX.
func ValidateStorageClassRWX(ctx context.Context, k8s kubernetes.Interface, scName string) error {
	sc, err := k8s.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("storage class %q not found: %w", scName, err)
	}
	if msg, blocked := rwoDenyList[sc.Provisioner]; blocked {
		return fmt.Errorf("storage class %q cannot be used for workspace PVC: %s", scName, msg)
	}
	return nil
}
