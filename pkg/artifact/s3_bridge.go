package artifact

import "github.com/NerdMeNot/flint/pkg/wsfs"

// newWSFSS3Impl creates an S3FS scoped to the artifacts prefix.
func newWSFSS3Impl(bucket, region string) (s3Client, error) {
	return wsfs.NewS3(bucket, region)
}
