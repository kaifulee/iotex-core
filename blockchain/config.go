// Copyright (c) 2022 IoTeX Foundation
// This source code is provided 'as is' and no warranties are given as to title or non-infringement, merchantability
// or fitness for purpose and, to the extent permitted by law, all liability for your use of the code is disclaimed.
// This source code is governed by Apache License 2.0 that can be found in the LICENSE file.

package blockchain

import (
	"crypto/ecdsa"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/iotexproject/go-pkgs/crypto"
	"github.com/iotexproject/iotex-address/address"
	"github.com/iotexproject/iotex-election/committee"
	"github.com/pkg/errors"
	"go.uber.org/config"
	"go.uber.org/zap"

	"github.com/iotexproject/iotex-core/v2/db"
	"github.com/iotexproject/iotex-core/v2/pkg/log"
)

type (
	// Config is the config struct for blockchain package
	Config struct {
		ChainDBPath            string `yaml:"chainDBPath"`
		TrieDBPatchFile        string `yaml:"trieDBPatchFile"`
		TrieDBPath             string `yaml:"trieDBPath"`
		StakingPatchDir        string `yaml:"stakingPatchDir"`
		IndexDBPath            string `yaml:"indexDBPath"`
		BloomfilterIndexDBPath string `yaml:"bloomfilterIndexDBPath"`
		CandidateIndexDBPath   string `yaml:"candidateIndexDBPath"`
		StakingIndexDBPath     string `yaml:"stakingIndexDBPath"`
		// deprecated
		SGDIndexDBPath             string           `yaml:"sgdIndexDBPath"`
		ContractStakingIndexDBPath string           `yaml:"contractStakingIndexDBPath"`
		BlobStoreDBPath            string           `yaml:"blobStoreDBPath"`
		BlobStoreRetentionDays     uint32           `yaml:"blobStoreRetentionDays"`
		HistoryIndexPath           string           `yaml:"historyIndexPath"`
		ID                         uint32           `yaml:"id"`
		EVMNetworkID               uint32           `yaml:"evmNetworkID"`
		Address                    string           `yaml:"address"`
		ProducerPrivKey            string           `yaml:"producerPrivKey"`
		ProducerPrivKeySchema      string           `yaml:"producerPrivKeySchema"`
		ProducerPrivKeyRange       string           `yaml:"producerPrivKeyRange"`
		SignatureScheme            []string         `yaml:"signatureScheme"`
		EmptyGenesis               bool             `yaml:"emptyGenesis"`
		GravityChainDB             db.Config        `yaml:"gravityChainDB"`
		Committee                  committee.Config `yaml:"committee"`

		// EnableTrielessStateDB enables trieless state db (deprecated)
		EnableTrielessStateDB bool `yaml:"enableTrielessStateDB"`
		// EnableStateDBCaching enables cachedStateDBOption
		EnableStateDBCaching bool `yaml:"enableStateDBCaching"`
		// EnableArchiveMode is only meaningful when EnableTrielessStateDB is false
		EnableArchiveMode bool `yaml:"enableArchiveMode"`
		// EnableAsyncIndexWrite enables writing the block actions' and receipts' index asynchronously
		EnableAsyncIndexWrite bool `yaml:"enableAsyncIndexWrite"`
		// deprecated
		EnableSystemLogIndexer bool `yaml:"enableSystemLog"`
		// EnableStakingProtocol enables staking protocol
		EnableStakingProtocol bool `yaml:"enableStakingProtocol"`
		// EnableStakingIndexer enables staking indexer
		EnableStakingIndexer bool `yaml:"enableStakingIndexer"`
		// AllowedBlockGasResidue is the amount of gas remained when block producer could stop processing more actions
		AllowedBlockGasResidue uint64 `yaml:"allowedBlockGasResidue"`
		// MaxCacheSize is the max number of blocks that will be put into an LRU cache. 0 means disabled
		MaxCacheSize int `yaml:"maxCacheSize"`
		// PollInitialCandidatesInterval is the config for committee init db
		PollInitialCandidatesInterval time.Duration `yaml:"pollInitialCandidatesInterval"`
		// StateDBCacheSize is the max size of statedb LRU cache
		StateDBCacheSize int `yaml:"stateDBCacheSize"`
		// WorkingSetCacheSize is the max size of workingset cache in state factory
		WorkingSetCacheSize uint64 `yaml:"workingSetCacheSize"`
		// StreamingBlockBufferSize
		StreamingBlockBufferSize uint64 `yaml:"streamingBlockBufferSize"`
		// PersistStakingPatchBlock is the block to persist staking patch
		PersistStakingPatchBlock uint64 `yaml:"persistStakingPatchBlock"`
		// FixAliasForNonStopHeight is the height to fix candidate alias for a non-stopping node
		FixAliasForNonStopHeight uint64 `yaml:"fixAliasForNonStopHeight"`
		// FactoryDBType is the type of factory db
		FactoryDBType string `yaml:"factoryDBType"`
		// MintTimeout is the timeout for minting
		MintTimeout time.Duration `yaml:"-"`
	}
)

var (
	// DefaultConfig is the default config of chain
	DefaultConfig = Config{
		ChainDBPath:                "/var/data/chain.db",
		TrieDBPatchFile:            "/var/data/trie.db.patch",
		TrieDBPath:                 "/var/data/trie.db",
		StakingPatchDir:            "/var/data",
		IndexDBPath:                "/var/data/index.db",
		BloomfilterIndexDBPath:     "/var/data/bloomfilter.index.db",
		CandidateIndexDBPath:       "/var/data/candidate.index.db",
		StakingIndexDBPath:         "/var/data/staking.index.db",
		SGDIndexDBPath:             "/var/data/sgd.index.db",
		ContractStakingIndexDBPath: "/var/data/contractstaking.index.db",
		BlobStoreDBPath:            "/var/data/blob.db",
		BlobStoreRetentionDays:     21,
		ID:                         1,
		EVMNetworkID:               4689,
		Address:                    "",
		ProducerPrivKey:            GenerateRandomKey(SigP256k1),
		SignatureScheme:            []string{SigP256k1},
		EmptyGenesis:               false,
		GravityChainDB:             db.Config{DbPath: "/var/data/poll.db", NumRetries: 10},
		Committee: committee.Config{
			GravityChainAPIs: []string{},
		},
		EnableTrielessStateDB:         true,
		EnableStateDBCaching:          false,
		EnableArchiveMode:             false,
		EnableAsyncIndexWrite:         true,
		EnableSystemLogIndexer:        false,
		EnableStakingProtocol:         true,
		EnableStakingIndexer:          false,
		AllowedBlockGasResidue:        10000,
		MaxCacheSize:                  0,
		PollInitialCandidatesInterval: 10 * time.Second,
		StateDBCacheSize:              1000,
		WorkingSetCacheSize:           20,
		StreamingBlockBufferSize:      200,
		PersistStakingPatchBlock:      19778037,
		FixAliasForNonStopHeight:      19778036,
		FactoryDBType:                 db.DBBolt,
		MintTimeout:                   700 * time.Millisecond,
	}

	// ErrConfig config error
	ErrConfig = errors.New("config error")
)

// ProducerAddress() returns the configured producer address derived from key
func (cfg *Config) ProducerAddress() []address.Address {
	privateKeys := cfg.ProducerPrivateKeys()
	addrs := make([]address.Address, 0, len(privateKeys))
	for _, sk := range privateKeys {
		addr := sk.PublicKey().Address()
		if addr == nil {
			log.L().Panic("Error when constructing producer address")
		}
		addrs = append(addrs, addr)
	}
	return addrs
}

// ProducerPrivateKeys returns the configured private keys
func (cfg *Config) ProducerPrivateKeys() []crypto.PrivateKey {
	pks := strings.Split(cfg.ProducerPrivKey, ",")
	if len(pks) == 0 {
		log.L().Panic("Error when decoding private key")
	}
	privateKeys := make([]crypto.PrivateKey, 0, len(pks))
	for _, pk := range pks {
		sk, err := crypto.HexStringToPrivateKey(pk)
		if err != nil {
			log.L().Panic(
				"Error when decoding private key",
				zap.Error(err),
			)
		}

		if !cfg.whitelistSignatureScheme(sk) {
			log.L().Panic("The private key's signature scheme is not whitelisted")
		}
		privateKeys = append(privateKeys, sk)
	}

	if cfg.ProducerPrivKeyRange == "" {
		return privateKeys
	}
	// Expecting format "[$start:$end]"
	r := strings.Trim(cfg.ProducerPrivKeyRange, "[]")
	parts := strings.Split(r, ":")
	if len(parts) != 2 {
		log.L().Panic("invalid format", zap.String("ProducerPrivKeyRange", cfg.ProducerPrivKeyRange))
	}
	start, end := 0, len(privateKeys)
	var err error
	if parts[0] != "" {
		start, err = strconv.Atoi(parts[0])
		if err != nil {
			log.L().Panic("invalid start", zap.String("start", parts[0]), zap.Error(err))
		}
	}
	if parts[1] != "" {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			log.L().Panic("invalid end", zap.String("end", parts[1]), zap.Error(err))
		}
	}
	if start < 0 || end > len(privateKeys) || start > end {
		log.L().Panic("ProducerPrivKeyRange out of bounds", zap.Int("start", start), zap.Int("end", end), zap.Int("len", len(privateKeys)))
	}

	return privateKeys[start:end]
}

// SetProducerPrivKey set producer privKey by PrivKeyConfigFile info
func (cfg *Config) SetProducerPrivKey() error {
	switch cfg.ProducerPrivKeySchema {
	case "hex", "":
		// do nothing
	case "hashiCorpVault":
		yaml, err := config.NewYAML(config.Expand(os.LookupEnv), config.File(cfg.ProducerPrivKey))
		if err != nil {
			return errors.Wrap(err, "failed to init private key config")
		}
		hcv := &hashiCorpVault{}
		if err := yaml.Get(config.Root).Populate(hcv); err != nil {
			return errors.Wrap(err, "failed to unmarshal YAML config to privKeyConfig struct")
		}

		loader, err := newVaultPrivKeyLoader(hcv)
		if err != nil {
			return errors.Wrap(err, "failed to new vault client")
		}
		key, err := loader.load()
		if err != nil {
			return errors.Wrap(err, "failed to load producer private key")
		}
		cfg.ProducerPrivKey = key
	default:
		return errors.Wrap(ErrConfig, "invalid private key schema")
	}

	return nil
}

// GenerateRandomKey generates a random private key based on the signature scheme
func GenerateRandomKey(scheme string) string {
	// generate a random key
	switch scheme {
	case SigP256k1:
		sk, _ := crypto.GenerateKey()
		return sk.HexString()
	case SigP256sm2:
		sk, _ := crypto.GenerateKeySm2()
		return sk.HexString()
	}
	return ""
}

func (cfg *Config) whitelistSignatureScheme(sk crypto.PrivateKey) bool {
	var sigScheme string

	switch sk.EcdsaPrivateKey().(type) {
	case *ecdsa.PrivateKey:
		sigScheme = SigP256k1
	case *crypto.P256sm2PrvKey:
		sigScheme = SigP256sm2
	}

	if sigScheme == "" {
		return false
	}
	for _, e := range cfg.SignatureScheme {
		if sigScheme == e {
			// signature scheme is whitelisted
			return true
		}
	}
	return false
}
