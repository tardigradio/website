package main

import (
	"path/filepath"

	"storj.io/storj/cmd/uplink/cmd"
	"storj.io/storj/pkg/miniogw"
	"storj.io/storj/pkg/provider"
)

func initConfig(homeDir string) *cmd.Config {
	//TODO: look at ServerConfig on provider.IdentityConfig. Do we need to set this?
	//TODO: these filepaths are deprecated
	identityCfg := provider.IdentityConfig{
		CertPath: filepath.Join(homeDir, ".storj/uplink/identity.cert"),
		KeyPath:  filepath.Join(homeDir, ".storj/uplink/identity.key"),
	}

	minioCfg := miniogw.MinioConfig{
		AccessKey: "3ee4E2vqy3myfKdPnuPKTQQavtqx",
		SecretKey: "3H1BL6sKtiRCrs9VxCbw9xboYsXp",
		Dir:  filepath.Join(homeDir, ".storj/uplink/miniogw"),
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

	eCfg := miniogw.EncryptionConfig{
		Key: "insertEncKeyHere",
		BlockSize: 1024,
		DataType: 1,
		PathType: 1,
	}

	storjCfg := miniogw.Config{
		identityCfg,
		minioCfg,
		clientCfg,
		rsCfg,
		eCfg,
	}

	return &cmd.Config{storjCfg}
}
