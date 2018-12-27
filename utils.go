package main

import (
	"os"
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
		AccessKey: os.Getenv("STORJACCESSKEY"),
		SecretKey: os.Getenv("STORJSECRETKEY"),
		Dir:       filepath.Join(homeDir, ".storj/uplink/miniogw"),
	}

	clientCfg := miniogw.ClientConfig{
		OverlayAddr:   "127.0.0.1:7778",
		PointerDBAddr: "127.0.0.1:7778",
		APIKey:        os.Getenv("STORJAPIKEY"),
		MaxInlineSize: 4096,
		SegmentSize:   64000000,
	}

	rsCfg := miniogw.RSConfig{
		MaxBufferMem:     0x400000,
		ErasureShareSize: 1024,
		MinThreshold:     2,
		RepairThreshold:  3,
		SuccessThreshold: 4,
		MaxThreshold:     5,
	}

	eCfg := miniogw.EncryptionConfig{
		Key:       os.Getenv("STORJENCRYPTIONKEY"),
		BlockSize: 1024,
		DataType:  1,
		PathType:  1,
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
