package types

import (
	"errors"
	"github.com/hpb-project/sphinx/common/crypto"
	"github.com/hpb-project/sphinx/common/log"
	"runtime"
)

var (
	ErrSignCheckFailed = errors.New("recover pubkey failed")
)

// result for recover pubkey
type RecoverPubkey struct {
	TxHash []byte // recover tx's hash
	Hash   []byte
	Sig    []byte // signature
	Pub    []byte // recovered pubkey
}

type QSRecoverPubKeyFunc func(RecoverPubkey, error)

type TaskTh struct {
	isFull bool
	queue  chan RecoverPubkey
}

type postParam struct {
	rs  RecoverPubkey
	err error
}

type QSHandle struct {
	bInit     bool
	bcontinue bool
	rpFunc    QSRecoverPubKeyFunc // callback function for external module
	maxThNum  int                 // thread number for soft ecc recover.
	thPool    []*TaskTh           // task pool
	postCh    chan postParam
	idx       int
}

var (
	qsHandle = &QSHandle{bInit: false, rpFunc: nil, maxThNum: 2}
)

func QSGetInstance() *QSHandle {
	return qsHandle
}

// receive msg and call rpFunc to external module
func postCallback(qs *QSHandle) {

	for {
		select {
		case p, ok := <-qs.postCh:
			if !ok {
				return
			}
			if qs.rpFunc != nil {
				qs.rpFunc(p.rs, p.err)
			}
		}
	}
}

func (qs *QSHandle) postResult(rs *RecoverPubkey, err error) {
	post := postParam{rs: *rs, err: err}
	select {
	case qs.postCh <- post:
		return
	default:
		log.Debug("qs postResult", "channel is full", len(qs.postCh))
	}
}

func (qs *QSHandle) postToSoft(rs *RecoverPubkey) bool {

	for i := 0; i < qs.maxThNum; i++ {
		idx := (qs.idx + i) % qs.maxThNum
		select {
		case qs.thPool[idx].queue <- *rs:
			qs.idx++
			return true
		default:
			log.Debug("qs", "thPool ", idx, "is full", len(qs.thPool[idx].queue))
		}
	}
	return false
}

// receive task and execute soft ecc-recover
func (qs *QSHandle) asyncSoftRecoverPubTask(queue chan RecoverPubkey) {

	for {
		select {
		case rs, ok := <-queue:
			if !ok {
				return
			}
			pub, err := crypto.Ecrecover(rs.Hash, rs.Sig)
			if err == nil {
				copy(rs.Pub, pub)
			}
			qs.postResult(&rs, err)
		}
	}
}

func (qs *QSHandle) Init() error {
	if qs.bInit {
		return nil
	}

	qs.bcontinue = true
	// use 1/4 cpu
	if runtime.NumCPU()/4 > qs.maxThNum {
		qs.maxThNum = runtime.NumCPU() / 4
	}

	qs.thPool = make([]*TaskTh, qs.maxThNum)
	qs.postCh = make(chan postParam, 1000000)

	for i := 0; i < qs.maxThNum; i++ {
		qs.thPool[i] = &TaskTh{isFull: false, queue: make(chan RecoverPubkey, 100000)}

		go qs.asyncSoftRecoverPubTask(qs.thPool[i].queue)
	}

	go postCallback(qs)
	return nil
}

func (qs *QSHandle) Release() error {
	qs.bcontinue = false
	close(qs.postCh)
	for i := 0; i < qs.maxThNum; i++ {
		close(qs.thPool[i].queue)
	}
	return nil
}

func (qs *QSHandle) RegisterRecoverPubCallback(call QSRecoverPubKeyFunc) {
	qs.rpFunc = call
}

func softRecoverPubkey(hash []byte, r []byte, s []byte, v byte) ([]byte, error) {
	var (
		result = make([]byte, 65)
		sig    = make([]byte, 65)
	)
	copy(sig[32-len(r):32], r)
	copy(sig[64-len(s):64], s)
	sig[64] = v
	pub, err := crypto.Ecrecover(hash[:], sig)
	if err != nil {
		return nil, ErrSignCheckFailed
	}
	copy(result[:], pub[0:])
	return result, nil
}

func (qs *QSHandle) ASyncValidateSign(txhash []byte, hash []byte, r []byte, s []byte, v byte) error {
	rs := RecoverPubkey{TxHash: make([]byte, 32), Hash: make([]byte, 32), Sig: make([]byte, 65), Pub: make([]byte, 65)}
	copy(rs.TxHash, txhash)
	copy(rs.Hash, hash)
	copy(rs.Sig[32-len(r):32], r)
	copy(rs.Sig[64-len(s):64], s)
	rs.Sig[64] = v
	qs.postToSoft(&rs)

	return nil
}

func (qs *QSHandle) ValidateSign(hash []byte, r []byte, s []byte, v byte) ([]byte, error) {
	return softRecoverPubkey(hash, r, s, v)
}

func init() {
	QSGetInstance().Init()
}
