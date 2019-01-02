package main

import (
	"context"
	"os"
	"path/filepath"

	"storj.io/storj/cmd/uplink/cmd"
	"storj.io/storj/pkg/miniogw"
	"storj.io/storj/pkg/provider"
)

func writeCert(ctx context.Context, homeDir string) error {
	if _, err := os.Stat(filepath.Join(homeDir, ".tardigradio/identity.cert")); !os.IsNotExist(err) {
		return nil
	}

	if _, err := os.Stat(filepath.Join(homeDir, ".tardigradio/identity.key")); !os.IsNotExist(err) {
		return nil
	}

	ca := provider.CASetupConfig{
		CertPath:    filepath.Join(homeDir, ".tardigradio/identity.cert"),
		KeyPath:     filepath.Join(homeDir, ".tardigradio/identity.key"),
		Difficulty:  15,
		Timeout:     "5m",
		Overwrite:   false,
		Concurrency: 4,
	}
	i := provider.IdentitySetupConfig{
		CertPath:  filepath.Join(homeDir, ".tardigradio/identity.cert"),
		KeyPath:   filepath.Join(homeDir, ".tardigradio/identity.key"),
		Overwrite: false,
		Version:   "0",
	}

	return provider.SetupIdentity(ctx, ca, i)
}

func initConfig(homeDir string) cmd.Config {
	//TODO: look at ServerConfig on provider.IdentityConfig. Do we need to set this?
	//TODO: these filepaths are deprecated
	identityCfg := provider.IdentityConfig{
		CertPath: filepath.Join(homeDir, ".tardigradio/identity.cert"),
		KeyPath:  filepath.Join(homeDir, ".tardigradio/identity.key"),
	}

	minioCfg := miniogw.MinioConfig{
		AccessKey: os.Getenv("STORJACCESSKEY"),
		SecretKey: os.Getenv("STORJSECRETKEY"),
	}

	clientCfg := miniogw.ClientConfig{
		OverlayAddr:   os.Getenv("STORJOVERLAYADDR"),
		PointerDBAddr: os.Getenv("STORJPOINTERDBADDR"),
		APIKey:        os.Getenv("STORJAPIKEY"),
		MaxInlineSize: 4096,
		SegmentSize:   64000000,
	}

	rsCfg := miniogw.RSConfig{
		MaxBufferMem:     0x400000,
		ErasureShareSize: 1024,
		MinThreshold:     4,
		RepairThreshold:  6,
		SuccessThreshold: 8,
		MaxThreshold:     10,
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

	return cmd.Config{storjCfg}
}
