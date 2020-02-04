package prometheus

import (
	"github.com/hashicorp/golang-lru"
	"github.com/hpb-project/sphinx/account"
	"github.com/hpb-project/sphinx/blockchain/storage"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/config"
	"github.com/hpb-project/sphinx/node/db"
	"math/big"
	"sync"
	"time"
)

// constant parameter definition
const (
	checkpointInterval    = 1024 // voting interval
	inmemoryHistorysnaps  = 128
	inmemorySignatures    = 4096
	wiggleTime            = 1000 * time.Millisecond
	comCheckpointInterval = 2
	cadCheckpointInterval = 2
)

// Prometheus protocol constants.
var (
	epochLength   = uint64(30000)
	blockPeriod   = uint64(15)               // default block interval is 15 seconds
	diffInTurn    = big.NewInt(2)            // the node is in turn, and its diffcult number is 2
	diffNoTurn    = big.NewInt(1)            // the node is not in turn, and its diffcult number is 1
	reentryMux    sync.Mutex
	insPrometheus *Prometheus
)

type PeerProof struct {
	Latest   int64 //time stamp for update.
	RootHash common.Hash
}

type Prometheus struct {
	config *config.PrometheusConfig // Consensus config
	db     hpbdb.Database           // Database

	recents    *lru.ARCCache // the recent signature
	signatures *lru.ARCCache // the last signature

	proposals map[common.Address]bool // current proposals (hpb nodes)
	proofs    sync.Map                // map[common.Address]PeerProof
	signer    common.Address
	signFn    SignerFn     // Callback function
	lock      sync.RWMutex // Protects the signerHash fields
}

func New(config *config.PrometheusConfig, db hpbdb.Database) *Prometheus {

	conf := *config

	if conf.Epoch == 0 {
		conf.Epoch = epochLength
	}

	recents, _ := lru.NewARC(inmemoryHistorysnaps)
	signatures, _ := lru.NewARC(inmemorySignatures)

	return &Prometheus{
		config:     &conf,
		db:         db,
		recents:    recents,
		signatures: signatures,
		proposals:  make(map[common.Address]bool),
	}
}

// InstanceBlockChain returns the singleton of BlockChain.
func InstancePrometheus() *Prometheus {
	if nil == insPrometheus {
		reentryMux.Lock()
		if nil == insPrometheus {
			insPrometheus = New(&config.GetHpbConfigInstance().Prometheus, db.GetHpbDbInstance())
		}
		reentryMux.Unlock()
	}
	return insPrometheus
}

type SignerFn func(accounts.Account, []byte) ([]byte, error)
