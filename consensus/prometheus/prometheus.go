package prometheus

import (
	"bytes"
	"errors"
	"github.com/hpb-project/sphinx/account"
	"github.com/hpb-project/sphinx/blockchain/state"
	"github.com/hpb-project/sphinx/blockchain/types"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/consensus"

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

	number := header.Number.Uint64()
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}

	header.Time = new(big.Int).Add(parent.Time, new(big.Int).SetUint64(c.config.Period))
	if header.Time.Int64() < time.Now().Unix() {
		header.Time = big.NewInt(time.Now().Unix())
	}
	header.Difficulty = big.NewInt(1)

	return nil
}

func (c *Prometheus)MixHash(first, second common.Hash) common.Hash {
	var result common.Hash
	for i := 0; i < common.HashLength; i++ {
		result[i] = first[i] ^ second[i]
	}
	return result
}

// GenerateProof
func (c *Prometheus) GenerateProof(chain consensus.ChainReader, header *types.Header, txs types.Transactions) (*types.WorkProof, error) {
	number := header.Number.Uint64()
	parent := chain.GetHeaderByNumber(number - 1)
	if parent == nil {
		return nil, errors.New("unknown parent")
	}
	lastHash := parent.ProofHash

	txroot := types.DeriveSha(txs)
	proofHash := c.MixHash(lastHash, txroot)
	signer, signFn := c.signer, c.signFn
	sighash, err := signFn(accounts.Account{Address: signer}, proofHash.Bytes())
	if err != nil {
		return nil, err
	}
	header.ProofHash = proofHash
	return &types.WorkProof{sighash, txs}, nil
}

// VerifyProof
func (c *Prometheus) VerifyProof(addr common.Address, initHash common.Hash, proof *types.WorkProof, update bool) error {
	if val, ok := c.proofs.Load(addr); ok {
		pf := val.(PeerProof)
		if hash, err := c.verifyProof(pf.RootHash, addr, proof); err == nil && update {
			c.UpdateProof(addr, hash)
		} else {
			return err
		}

	} else {
		pf := &PeerProof{time.Now().Unix(), initHash}
		c.proofs.Store(addr, pf)
		if hash, err := c.verifyProof(pf.RootHash, addr, proof); err == nil && update {
			c.UpdateProof(addr, hash)
		} else {
			return err
		}
	}
}

func (c *Prometheus) UpdateProof(addr common.Address, hash common.Hash) {
	if val, ok := c.proofs.Load(addr); ok {
		if pf, ok := val.(*PeerProof); ok {
			pf.RootHash = hash
			pf.Latest = time.Now().Unix()
		}
	} else {
		pf := &PeerProof{time.Now().Unix(), hash}
		c.proofs.Store(addr, pf)
	}
}
