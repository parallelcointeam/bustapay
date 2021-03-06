package main

import (
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcd/wire"
	"encoding/json"
	"bytes"
	"encoding/hex"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/btcjson"
	"fmt"
	"errors"
)

// This is a wrapper around btcd/rpcclient to make it a bit easier to use
type RpcClient struct {
	rpcClient *rpcclient.Client
}

/// The caller must always becareful to call client.Shutdown()!
func NewRpcClient() (*RpcClient, error) {

	cfg := &rpcclient.ConnConfig{
		Host:         "localhost:18332",
		User:         "Ulysseys",
		Pass:         "ZWHqhL4Xf9JMokLPzd4eeD",
		HTTPPostMode: true, // Bitcoin only supports HTTP POST mode
		DisableTLS:   true, // Bitcoin does not provide TLS by default
	}

	rpcClient, err := rpcclient.New(cfg, nil)
	if err != nil {
		return nil, err
	}

	return &RpcClient{rpcClient: rpcClient}, nil
}

func (rc *RpcClient) Shutdown() {
	rc.rpcClient.Shutdown()
}

// hacky, eats errors...
func (rc *RpcClient) MempoolHasEntry(txid string) bool {
	entry, err := rc.rpcClient.GetMempoolEntry(txid)
	return err == nil && entry != nil

}


func (rc *RpcClient) CreateRawTransaction(address string, amount int64) (*wire.MsgTx, error) {
	addr, err := btcutil.DecodeAddress(address, nil)
	if err != nil {
		return nil, err
	}

	outputs := make(map[btcutil.Address]btcutil.Amount, 1)
	outputs[addr] = btcutil.Amount(amount)

	return rc.rpcClient.CreateRawTransaction(nil, outputs, nil)
}


type FRTResult struct {
	Hex string `json:"hex"`
}

func (rc *RpcClient) FundRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, error) {

	byteBuffer := bytes.Buffer{}
	if err := tx.Serialize(&byteBuffer); err != nil {
		return nil, err
	}

	j, err := json.Marshal(hex.EncodeToString(byteBuffer.Bytes()))
	if err != nil {
		return nil, err
	}

	rm, err := rc.rpcClient.RawRequest("fundrawtransaction", []json.RawMessage{ j })
	if err != nil {
		return nil, err
	}

	res := FRTResult{}
	if err := json.Unmarshal(rm, &res); err != nil {
		return nil, err
	}

	serializedTx, err := hex.DecodeString(res.Hex)
	if err != nil {
		return nil, err
	}

	var msgTx wire.MsgTx
	if err := msgTx.Deserialize(bytes.NewReader(serializedTx)); err != nil {
		return nil, err
	}


	return &msgTx, nil
}

func (rc *RpcClient) SendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	return rc.rpcClient.SendRawTransaction(tx, false)
}



func (rc *RpcClient) SignRawTransactionWithWallet(tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
	txByteBuffer := bytes.Buffer{}
	err := tx.Serialize(&txByteBuffer)
	if err != nil {
		return nil, false, err
	}

	jsonData, err := json.Marshal(hex.EncodeToString(txByteBuffer.Bytes()))
	if err != nil {
		return nil, false, err
	}

	resultJson, err := rc.rpcClient.RawRequest("signrawtransactionwithwallet", []json.RawMessage{jsonData})
	if err != nil {
		return nil, false, err
	}

	var result SignRawTransactionResult
	err = json.Unmarshal(resultJson, &result)
	if err != nil {
		return nil, false, err
	}

	txBytes, err := hex.DecodeString(result.Hex)
	if err != nil {
		return nil, false, err
	}

	newTx, err := btcutil.NewTxFromBytes(txBytes)
	if err != nil {
		return nil, false, err
	}

	return newTx.MsgTx(), result.Complete, nil
}

type SignRawTransactionResult struct {
	Hex      string        `json:"hex"`
	Complete bool          `json:"complete"`
	Errors   []interface{} `json:"errors"`
}

// Note this is only segwit compatible. Don't use it to sign non-segwit inputs
func (rc *RpcClient) SafeSignRawTransactionWithWallet(tx *wire.MsgTx, inputToSign int) (*wire.MsgTx, bool, error) {
	res, complete, err := rc.SignRawTransactionWithWallet(tx)
	if err != nil {
		return nil, false, err
	}


	for i := 0; i < len(tx.TxIn); i++ {
		originalTxIn := tx.TxIn[i]
		newTxIn := res.TxIn[i]


		we := witnessEqual(originalTxIn.Witness, newTxIn.Witness)

		if i == inputToSign {
			if we {
				return nil, false, errors.New(fmt.Sprint("witness did not change for input ", i, " in tx ", tx.TxHash().String(), " that we should have signed"))
			}
		} else if !we {
			return nil, false, errors.New(fmt.Sprint("witness changed for input ", i, " in tx ", tx.TxHash().String(), " but we should have only signed ", inputToSign))
		} else if !bytes.Equal(originalTxIn.SignatureScript, newTxIn.SignatureScript) {
			return nil, false, errors.New(fmt.Sprint("signature script changed for input ", i, " in tx ", tx.TxHash().String()))
		}
	}

	return res, complete, nil
}

func witnessEqual(w1 wire.TxWitness, w2 wire.TxWitness) bool {
	if len(w1) != len(w2) {
		return false
	}

	for i := 0; i < len(w1); i++ {
		sw1 := w1[0]
		sw2 := w2[1]

		if !bytes.Equal(sw1, sw2) {
			return false
		}
	}
	return true
}

func (rc *RpcClient) GetTxOut(txid *chainhash.Hash, vout uint32) (*btcjson.GetTxOutResult, error) {
	return rc.rpcClient.GetTxOut(txid, vout, false)
}

type MemPoolAcceptResult struct {
	Txid         string `json:"txid"`
	Allowed      bool   `json:"allowed"`
	RejectReason string `json:"reject-reason"`
}

func (rc *RpcClient) TestMempoolAccept(tx *wire.MsgTx) (bool, error) {
	// NOTE: requires bitcoin core 0.17.x

	txByteBuffer := bytes.Buffer{}
	err := tx.Serialize(&txByteBuffer)
	if err != nil {
		return false, err
	}

	jsonData, err := json.Marshal([]string{hex.EncodeToString(txByteBuffer.Bytes())})
	if err != nil {
		return false, err
	}

	resultJson, err := rc.rpcClient.RawRequest("testmempoolaccept", []json.RawMessage{jsonData})
	if err != nil {
		return false, err
	}

	var result []MemPoolAcceptResult
	err = json.Unmarshal(resultJson, &result)
	if err != nil {
		return false, err
	}


	return result[0].Allowed, nil
}


// This is extremely unoptimized! It will be painful on a large wallet
func (rc *RpcClient) IsMyFreshMyAddress(address string) (bool, error) {

	info, err :=  rc.GetAddressInfo(address)
	if err != nil {
		return false, nil
	}

	if !info.IsMine {
		return false, nil
	}

	receives, err := rc.rpcClient.ListReceivedByAddress()
	if err != nil {
		return false, err
	}

	for _, receive := range receives {
		if receive.Address == address { // this address has been used before :/
			return false, err
		}

	}


	return  true, nil
}

func (rc *RpcClient) ListUnspent() ([]btcjson.ListUnspentResult, error) {

	return rc.rpcClient.ListUnspent()
}


type AddressInfoResult struct {
	Address string `json:"address"`
	IsMine  bool   `json:"ismine"`
}
func (rc *RpcClient) GetAddressInfo(address string) (*AddressInfoResult, error) {

	jsonData, err := json.Marshal(address)
	if err != nil {
		return nil, err
	}

	resultJson, err := rc.rpcClient.RawRequest("getaddressinfo", []json.RawMessage{jsonData})
	if err != nil {
		return nil, err
	}

	var result AddressInfoResult
	err = json.Unmarshal(resultJson, &result)
	if err != nil {
		return nil, err
	}


	return &result, nil
}
