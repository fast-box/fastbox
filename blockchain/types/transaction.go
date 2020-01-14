// Copyright 2018 The go-hpb Authors
// Modified based on go-ethereum, which Copyright (C) 2014 The go-ethereum Authors.
//
// The go-hpb is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-hpb is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-hpb. If not, see <http://www.gnu.org/licenses/>.

package types

import (
	"container/heap"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync/atomic"

	"encoding/json"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/common/crypto"
	"github.com/hpb-project/sphinx/common/crypto/sha3"
	"github.com/hpb-project/sphinx/common/hexutil"
	"github.com/hpb-project/sphinx/common/rlp"
	"github.com/hpb-project/sphinx/config"
)

//go:generate gencodec -type txdata -field-override txdataMarshaling -out gen_tx_json.go

var (
	ErrInvalidSig = errors.New("invalid transaction v, r, s values")
	errNoSigner   = errors.New("missing signing methods")
)

// deriveSigner makes a *best* guess about which signer to use.
func deriveSigner(V *big.Int) Signer {
	return NewQSSigner(deriveChainId(V))
}

type Transaction struct {
	data txdata
	// caches
	hash atomic.Value
	size atomic.Value
	from atomic.Value
}

type txdata struct {
	Payload []byte `json:"input"    gencodec:"required"`

	// Signature values
	V *big.Int `json:"v" gencodec:"required"`
	R *big.Int `json:"r" gencodec:"required"`
	S *big.Int `json:"s" gencodec:"required"`

	Forward bool `json:"forward" rlp:"-"`
	// This is only used when marshaling to JSON.
	Hash *common.Hash `json:"hash" rlp:"-"`
}

type txdataMarshaling struct {
	Payload hexutil.Bytes
	V       *hexutil.Big
	R       *hexutil.Big
	S       *hexutil.Big
}

func NewTransaction(data []byte) *Transaction {
	return newTransaction(data)
}

func newTransaction(data []byte) *Transaction {
	if len(data) > 0 {
		data = common.CopyBytes(data)
	}
	d := txdata{
		Payload: data,
		V:       new(big.Int),
		R:       new(big.Int),
		S:       new(big.Int),
	}

	return &Transaction{data: d}
}

// ChainId returns which chain id this transaction was signed for (if at all)
func (tx *Transaction) ChainId() *big.Int {
	return deriveChainId(tx.data.V)
}

// Protected returns whether the transaction is protected from replay protection.
func (tx *Transaction) Protected() bool {
	return isProtectedV(tx.data.V)
}

func isProtectedV(V *big.Int) bool {
	if V.BitLen() <= 8 {
		v := V.Uint64()
		return v != 27 && v != 28
	}
	// anything not 27 or 28 are considered unprotected
	return true
}

// DecodeRLP implements rlp.Encoder
func (tx *Transaction) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &tx.data)
}

// DecodeRLP implements rlp.Decoder
func (tx *Transaction) DecodeRLP(s *rlp.Stream) error {
	_, size, _ := s.Kind()
	err := s.Decode(&tx.data)
	if err == nil {
		tx.size.Store(common.StorageSize(rlp.ListSize(size)))
	}

	return err
}

// IntrinsicGas computes the 'intrinsic gas' for a message
// with the given data.
func IntrinsicGas(data []byte, contractCreation bool) *big.Int {
	igas := new(big.Int)
	if contractCreation {
		igas.SetUint64(config.TxGasContractCreation)
	} else {
		igas.SetUint64(config.TxGas)
	}
	if len(data) > 0 {
		var nz int64
		for _, byt := range data {
			if byt != 0 {
				nz++
			}
		}
		m := big.NewInt(nz)
		m.Mul(m, new(big.Int).SetUint64(config.TxDataNonZeroGas))
		igas.Add(igas, m)
		m.SetInt64(int64(len(data)) - nz)
		m.Mul(m, new(big.Int).SetUint64(config.TxDataZeroGas))
		igas.Add(igas, m)
	}
	return igas
}

func (t txdata) MarshalJSON() ([]byte, error) {
	type txdata struct {
		Payload hexutil.Bytes `json:"input"    gencodec:"required"`
		V       *hexutil.Big  `json:"v" gencodec:"required"`
		R       *hexutil.Big  `json:"r" gencodec:"required"`
		S       *hexutil.Big  `json:"s" gencodec:"required"`
		Hash    *common.Hash  `json:"hash" rlp:"-"`
	}
	var enc txdata
	enc.Payload = t.Payload
	enc.V = (*hexutil.Big)(t.V)
	enc.R = (*hexutil.Big)(t.R)
	enc.S = (*hexutil.Big)(t.S)
	enc.Hash = t.Hash
	return json.Marshal(&enc)
}

func (t *txdata) UnmarshalJSON(input []byte) error {
	type txdata struct {
		Payload hexutil.Bytes `json:"input"    gencodec:"required"`
		V       *hexutil.Big  `json:"v" gencodec:"required"`
		R       *hexutil.Big  `json:"r" gencodec:"required"`
		S       *hexutil.Big  `json:"s" gencodec:"required"`
		Hash    *common.Hash  `json:"hash" rlp:"-"`
	}
	var dec txdata
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	t.Payload = dec.Payload
	if dec.V == nil {
		return errors.New("missing required field 'v' for txdata")
	}
	t.V = (*big.Int)(dec.V)
	if dec.R == nil {
		return errors.New("missing required field 'r' for txdata")
	}
	t.R = (*big.Int)(dec.R)
	if dec.S == nil {
		return errors.New("missing required field 's' for txdata")
	}
	t.S = (*big.Int)(dec.S)
	if dec.Hash != nil {
		t.Hash = dec.Hash
	}
	return nil
}

func (tx *Transaction) MarshalJSON() ([]byte, error) {
	hash := tx.Hash()
	data := tx.data
	data.Hash = &hash
	return data.MarshalJSON()
}

// UnmarshalJSON decodes the web3 RPC transaction format.
func (tx *Transaction) UnmarshalJSON(input []byte) error {
	var dec txdata
	if err := dec.UnmarshalJSON(input); err != nil {
		return err
	}
	var V byte
	if isProtectedV(dec.V) {
		chainId := deriveChainId(dec.V).Uint64()
		V = byte(dec.V.Uint64() - 35 - 2*chainId)
	} else {
		V = byte(dec.V.Uint64() - 27)
	}
	if !crypto.ValidateSignatureValues(V, dec.R, dec.S, false) {
		return ErrInvalidSig
	}
	*tx = Transaction{data: dec}
	return nil
}

func (tx *Transaction) Data() []byte     { return common.CopyBytes(tx.data.Payload) }
func (tx *Transaction) CheckNonce() bool { return true }

func (tx *Transaction) SetFrom(from common.Address) { tx.from.Store(from) }
func (tx *Transaction) SetForward(forward bool)     { tx.data.Forward = forward }
func (tx *Transaction) IsForward() bool             { return tx.data.Forward }

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

// Hash hashes the RLP encoding of tx.
// It uniquely identifies the transaction.
func (tx *Transaction) Hash() common.Hash {
	if hash := tx.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	temp := tx.IsForward()
	tx.SetForward(false)
	v := rlpHash(tx)
	tx.SetForward(temp)
	tx.hash.Store(v)
	return v
}

type writeCounter common.StorageSize

func (c *writeCounter) Write(b []byte) (int, error) {
	*c += writeCounter(len(b))
	return len(b), nil
}

func (tx *Transaction) Size() common.StorageSize {
	if size := tx.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, &tx.data)
	tx.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

// WithSignature returns a new transaction with the given signature.
// This signature needs to be formatted as described in the yellow paper (v+27).
func (tx *Transaction) WithSignature(signer Signer, sig []byte) (*Transaction, error) {
	r, s, v, err := signer.SignatureValues(tx, sig)
	if err != nil {
		return nil, err
	}

	cpy := &Transaction{data: tx.data}
	cpy.data.R, cpy.data.S, cpy.data.V = r, s, v
	return cpy, nil
}

func (tx *Transaction) RawSignatureValues() (*big.Int, *big.Int, *big.Int) {
	return tx.data.V, tx.data.R, tx.data.S
}

func (tx *Transaction) String() string {
	var from string
	if tx.data.V != nil {
		// make a best guess about the signer and use that to derive
		// the sender.
		signer := deriveSigner(tx.data.V)
		if f, err := Sender(signer, tx); err != nil { // derive but don't cache
			from = "[invalid sender: invalid sig]"
		} else {
			from = fmt.Sprintf("%x", f[:])
		}
	} else {
		from = "[invalid sender: nil V field]"
	}
	enc, _ := rlp.EncodeToBytes(&tx.data)
	return fmt.Sprintf(`
	TX(%x)
	From:     %s
	Data:     0x%x
	V:        %#x
	R:        %#x
	S:        %#x
	Hex:      %x
`,
		tx.Hash(),
		from,
		tx.data.Payload,
		tx.data.V,
		tx.data.R,
		tx.data.S,
		enc,
	)
}

// Transaction slice type for basic sorting.
type Transactions []*Transaction

// Len returns the length of s
func (s Transactions) Len() int { return len(s) }

// Swap swaps the i'th and the j'th element in s
func (s Transactions) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

// GetRlp implements Rlpable and returns the i'th element of s in rlp
func (s Transactions) GetRlp(i int) []byte {
	enc, _ := rlp.EncodeToBytes(s[i])
	return enc
}

// Returns a new set t which is the difference between a to b
func TxDifference(a, b Transactions) (keep Transactions) {
	keep = make(Transactions, 0, len(a))

	remove := make(map[common.Hash]struct{})
	for _, tx := range b {
		remove[tx.Hash()] = struct{}{}
	}

	for _, tx := range a {
		if _, ok := remove[tx.Hash()]; !ok {
			keep = append(keep, tx)
		}
	}

	return keep
}

type TxByPayload Transactions

func (s TxByPayload) Len() int           { return len(s) }
func (s TxByPayload) Less(i, j int) bool { return len(s[i].data.Payload) < len(s[j].data.Payload) }
func (s TxByPayload) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s *TxByPayload) Push(x interface{}) {
	*s = append(*s, x.(*Transaction))
}

func (s *TxByPayload) Pop() interface{} {
	old := *s
	n := len(old)
	x := old[n-1]
	*s = old[0 : n-1]
	return x
}

type TransactionsByPayload struct {
	txs    map[common.Address]Transactions // Per account nonce-sorted list of transactions
	heads  TxByPayload                     // Next transaction for each unique account (price heap)
	signer Signer                          // Signer for the set of transactions
}

// NewTransactionsByPayload creates a transaction set that can retrieve
// payload length sorted transactions.
//
// Note, the input map is reowned so the caller should not interact any more with
// if after providing it to the constructor.
func NewTransactionsByPayload(signer Signer, txs map[common.Address]Transactions) *TransactionsByPayload {
	// Initialize a price based heap with the head transactions
	var block_max_txs = 50000
	var cnt = 0
	heads := make(TxByPayload, 0, len(txs))
	for _, accTxs := range txs {
		cnt += 1
		if cnt >= block_max_txs {
			break
		}
		if accTxs != nil && len(accTxs) > 0 {
			heads = append(heads, accTxs[0])
			// Ensure the sender address is from the signer
			acc, _ := Sender(signer, accTxs[0])
			txs[acc] = accTxs[1:]
		}
	}
	heap.Init(&heads)

	// Assemble and return the transaction set
	return &TransactionsByPayload{
		txs:    txs,
		heads:  heads,
		signer: signer,
	}
}

// Peek returns the next transaction by price.
func (t *TransactionsByPayload) Peek() *Transaction {
	if len(t.heads) == 0 {
		return nil
	}
	return t.heads[0]
}

// Shift replaces the current best head with the next one from the same account.
func (t *TransactionsByPayload) Shift() {
	acc, _ := Sender(t.signer, t.heads[0])
	if txs, ok := t.txs[acc]; ok && len(txs) > 0 {
		t.heads[0], t.txs[acc] = txs[0], txs[1:]
		heap.Fix(&t.heads, 0)
	} else {
		heap.Pop(&t.heads)
	}
}

// Pop removes the best transaction, *not* replacing it with the next one from
// the same account. This should be used when a transaction cannot be executed
// and hence all subsequent ones should be discarded from the same account.
func (t *TransactionsByPayload) Pop() {
	heap.Pop(&t.heads)
}
