package prometheus

import (
	"bytes"
	"encoding/hex"
	"errors"
	"github.com/shx-project/sphinx/account"
	"github.com/shx-project/sphinx/blockchain/state"
	"github.com/shx-project/sphinx/blockchain/types"
	"github.com/shx-project/sphinx/common"
	"github.com/shx-project/sphinx/common/crypto/sha3"
	"github.com/shx-project/sphinx/common/log"
	"github.com/shx-project/sphinx/consensus"
	"gopkg.in/fatih/set.v0"

	"math/big"
	"time"
)



// Prepare function for Block
func (c *Prometheus) PrepareBlockHeader(chain consensus.ChainReader, header *types.Header, state *state.StateDB) error {

	// check the header
	if len(header.Extra) < consensus.ExtraVanity {
		header.Extra = append(header.Extra, bytes.Repeat([]byte{0x00}, consensus.ExtraVanity-len(header.Extra))...)
	}

	header.Extra = header.Extra[:consensus.ExtraVanity]

	header.Extra = append(header.Extra, make([]byte, consensus.ExtraSeal)...)

	header.Time = big.NewInt(time.Now().UnixNano()/1000/1000)
	header.Difficulty = big.NewInt(1)

	return nil
}

func (c *Prometheus)MixHash(first, second common.Hash) (h common.Hash) {
	hw := sha3.NewKeccak256()
	data := make([]byte,len(first) +len(second))
	copy(data,first[:])
	copy(data[len(first):],second[:])
	hw.Write(data)
	hw.Sum(h[:0])
	return
}

// GenerateProof
func (c *Prometheus) GenerateProof(chain consensus.ChainReader, header *types.Header,parent *types.Header, txs types.Transactions, proofs types.ProofStates) (*types.WorkProof, error) {
	number := header.Number.Uint64()
	log.Debug("prometheus generate proof","parent.hash", parent.Hash(), "parent.ProofHash", hex.EncodeToString(parent.ProofHash[:]))

	if parent == nil {
		if parent = chain.GetHeaderByNumber(number - 1); parent == nil {
			return nil, errors.New("unknown parent")
		}
	}
	lastHash := parent.ProofHash

	txroot := types.DeriveSha(txs)
	//proofRoot := types.DeriveSha(proofs)
	//proofHash := c.MixHash(txroot, proofRoot)
	proofHash := c.MixHash(lastHash, txroot)

	signer, signFn := c.signer, c.signFn
	sighash, err := signFn(accounts.Account{Address: signer}, proofHash.Bytes())
	if err != nil {
		return nil, err
	}
	header.TxHash = txroot
	header.ProofHash = proofHash

	log.Debug("prometheus generate proof","proofhash",hex.EncodeToString(sighash), "txroot", txroot, "localhash", lastHash)
	return &types.WorkProof{number,sighash, txs,proofs}, nil
}

func (c *Prometheus) VerifyProofQuick(lasthash common.Hash, txroot common.Hash, newHash common.Hash) error {
	newproofhash := c.MixHash(lasthash, txroot)
	if bytes.Compare(newproofhash[:], newHash[:]) == 0 {
		return nil
	}
	return errors.New("proof incorrect")
}

// VerifyProof
func (c *Prometheus) VerifyProof(addr common.Address, lastHash common.Hash, proof *types.WorkProof) (common.Hash, error) {
	return c.verifyProof(lastHash, addr, proof)
}


// VerifyState
func (c *Prometheus) VerifyState(coinbase common.Address, history *set.Set, proof *types.WorkProof) bool {
	return true
	for _, stat := range proof.States {
		if stat.Addr == coinbase {
			if history.Has(stat.Root) {
				return true
			} else {
				return false
			}
		}
	}
	return false
}
