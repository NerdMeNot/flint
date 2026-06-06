package cache

import "github.com/NerdMeNot/flint/pkg/wsfs"

func newS3Client(bucket, region string) (s3Client, error) {
	return wsfs.NewS3(bucket, region)
}
