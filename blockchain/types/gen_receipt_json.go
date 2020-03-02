// Code generated by github.com/fjl/gencodec. DO NOT EDIT.

package types

import (
	"encoding/json"
	"errors"
	"github.com/hpb-project/sphinx/common"
	"github.com/hpb-project/sphinx/common/hexutil"
)

func (r Receipt) MarshalJSON() ([]byte, error) {
	type Receipt struct {
		PostState hexutil.Bytes `json:"root"`
		Status    hexutil.Uint  `json:"status"`
		Bloom     Bloom         `json:"logsBloom"         gencodec:"required"`
		Logs      []*Log        `json:"logs"              gencodec:"required"`
		TxHash    common.Hash   `json:"transactionHash" gencodec:"required"`
		ConfirmCount 	hexutil.Uint64 `json:"confirmed" gencodec:"required"`
	}
	var enc Receipt
	enc.PostState = r.PostState
	enc.Status = hexutil.Uint(r.Status)
	enc.Bloom = r.Bloom
	enc.Logs = r.Logs
	enc.TxHash = r.TxHash
	enc.ConfirmCount = r.ConfirmCount
	return json.Marshal(&enc)
}

func (r *Receipt) UnmarshalJSON(input []byte) error {
	type Receipt struct {
		PostState hexutil.Bytes `json:"root"`
		Status    *hexutil.Uint `json:"status"`
		Bloom     *Bloom        `json:"logsBloom"         gencodec:"required"`
		Logs      []*Log        `json:"logs"              gencodec:"required"`
		TxHash    *common.Hash  `json:"transactionHash" gencodec:"required"`
		ConfirmCount 	*hexutil.Uint64 `json:"confirmed" gencodec:"required"`
	}
	var dec Receipt
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.PostState != nil {
		r.PostState = dec.PostState
	}
	if dec.Status != nil {
		r.Status = uint(*dec.Status)
	}
	if dec.Bloom == nil {
		return errors.New("missing required field 'logsBloom' for Receipt")
	}
	r.Bloom = *dec.Bloom
	if dec.Logs == nil {
		return errors.New("missing required field 'logs' for Receipt")
	}
	r.Logs = dec.Logs
	if dec.TxHash == nil {
		return errors.New("missing required field 'transactionHash' for Receipt")
	}
	r.TxHash = *dec.TxHash
	r.ConfirmCount = *dec.ConfirmCount
	return nil
}
