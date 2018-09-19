package main

import (
	"context"
	"path/filepath"

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
