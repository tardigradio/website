package main

import (
	"context"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"storj.io/storj/cmd/uplink/cmd"
	"storj.io/storj/pkg/miniogw"
	"storj.io/storj/pkg/provider"
	"storj.io/storj/pkg/storage/buckets"
)

func getBucketStore(ctx context.Context, homeDir string) (buckets.Store, error) {
	identityCfg := provider.IdentityConfig{
		CertPath: filepath.Join(homeDir, ".storj/uplink/identity.cert"),
		KeyPath:  filepath.Join(homeDir, ".storj/uplink/identity.key"),
		Address:  ":7777",
	}

	minioCfg := miniogw.MinioConfig{
		AccessKey: "3ee4E2vqy3myfKdPnuPKTQQavtqx",
		SecretKey: "3H1BL6sKtiRCrs9VxCbw9xboYsXp",
		MinioDir:  filepath.Join(homeDir, ".storj/uplink/miniogw"),
	}

	clientCfg := miniogw.ClientConfig{
		OverlayAddr:   "satellite.staging.storj.io:7777",
		PointerDBAddr: "satellite.staging.storj.io:7777",
		APIKey:        "CribRetrievableEyebrows",
		MaxInlineSize: 4096,
		SegmentSize:   64000000,
	}

	rsCfg := miniogw.RSConfig{
		MaxBufferMem:     0x400000,
		ErasureShareSize: 1024,
		MinThreshold:     20,
		RepairThreshold:  30,
		SuccessThreshold: 40,
		MaxThreshold:     50,
	}

	storjCfg := miniogw.Config{
		identityCfg,
		minioCfg,
		clientCfg,
		rsCfg,
	}

	cfg := &cmd.Config{storjCfg}

	return cfg.BucketStore(ctx)
}

func generatePresignedURL(key string, bucket string) (string, error) {
	defaultResolver := endpoints.DefaultResolver()
	s3CustResolverFn := func(service, region string, optFns ...func(*endpoints.Options)) (endpoints.ResolvedEndpoint, error) {
		if service == "s3" {
			return endpoints.ResolvedEndpoint{
				URL:           "satellite.staging.storj.io:7777",
				SigningRegion: "custom-signing-region",
			}, nil
		}

		return defaultResolver.EndpointFor(service, region, optFns...)
	}
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region:           aws.String("us-west-2"),
			EndpointResolver: endpoints.ResolverFunc(s3CustResolverFn),
		},
	}))

	// Create the S3 service client with the shared session. This will
	// automatically use the S3 custom endpoint configured in the custom
	// endpoint resolver wrapping the default endpoint resolver.
	s3Svc := s3.New(sess)
	// Operation calls will be made to the custom endpoint.
	req, _ := s3Svc.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	return req.Presign(15 * time.Minute)
}
