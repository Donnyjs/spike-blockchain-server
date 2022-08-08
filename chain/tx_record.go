package chain

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"math/big"
	"spike-blockchain-server/constants"
	"spike-blockchain-server/serializer"
	"strconv"
	"strings"
)

type NativeTransactionRecordService struct {
	Address string `form:"address" json:"address" binding:"required"`
}

type ERC20TransactionRecordService struct {
	Address         string `form:"address" json:"address" binding:"required"`
	ContractAddress string `form:"contract_address" json:"contract_address" binding:"required"`
}

type Result struct {
	Hash        string `json:"hash"`
	TimeStamp   string `json:"timeStamp"`
	BlockNumber string `json:"blockNumber"`
	BlockHash   string `json:"blockHash"`
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	Input       string `json:"input"`
	Type        string `json:"type"`
}

type BscRes struct {
	Status  string   `json:"status"`
	Message string   `json:"message"`
	Result  []Result `json:"result"`
}

func (bl *BscListener) NativeTxRecord(c *gin.Context) {
	var service NativeTransactionRecordService
	if err := c.ShouldBind(&service); err == nil {
		res := bl.findFindNativeTxRecord(service.Address)
		c.JSON(200, res)
	} else {
		c.JSON(500, serializer.Response{
			Code: 500,
			Msg:  ErrorParam.Error(),
		})
	}
}

func (bl *BscListener) ERC20TxRecord(c *gin.Context) {
	var service ERC20TransactionRecordService
	if err := c.ShouldBind(&service); err == nil {
		res := bl.findFindERC20TxRecord(service.Address, service.ContractAddress)
		c.JSON(200, res)
	} else {
		c.JSON(500, serializer.Response{
			Code: 500,
			Msg:  ErrorParam.Error(),
		})
	}
}

func (bl *BscListener) findFindERC20TxRecord(address, contractAddr string) serializer.Response {
	if record := bl.GetJson(address + contractAddr + erc20TxRecordSuffix); record != "" {
		var bscRes BscRes
		bnbRecord := make([]Result, 0)
		bscRes.Result = bnbRecord
		err := json.Unmarshal([]byte(record), &bscRes)
		if err != nil {
			return serializer.Response{
				Code: 500,
				Msg:  err.Error(),
			}
		}
		return serializer.Response{
			Code: 200,
			Data: bscRes,
		}
	}
	bscRes, err := bl.FindFindERC20TxRecord(contractAddr, address)
	if err != nil {
		return serializer.Response{
			Code: 500,
			Msg:  err.Error(),
		}
	}

	return serializer.Response{
		Code: 200,
		Data: bscRes,
	}
}

func (bl *BscListener) GetBlockNum() uint64 {
	if bl.rc.Get(BLOCK_NUM).Err() == redis.Nil {
		log.Infof("blockNum is not exist")
		blockNum, err := bl.ec.BlockNumber(context.Background())
		if err != nil {
			return 0
		}
		return blockNum
	} else {
		blockNum, _ := bl.rc.Get(BLOCK_NUM).Uint64()
		return blockNum
	}
}

func queryNativeTxRecord(address string, blockNum uint64) (BscRes, error) {
	bscRes := BscRes{Result: make([]Result, 0)}
	client := resty.New()
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		Get(getNativeUrl(blockNum, address))
	if err != nil {
		return bscRes, err
	}
	json.Unmarshal(resp.Body(), &bscRes)
	return bscRes, nil
}

func (bl *BscListener) findFindNativeTxRecord(address string) serializer.Response {
	if record := bl.GetJson(address + nativeTxRecordSuffix); record != "" {
		var bscRes BscRes
		bnbRecord := make([]Result, 0)
		bscRes.Result = bnbRecord
		err := json.Unmarshal([]byte(record), &bscRes)
		if err != nil {
			return serializer.Response{
				Code: 500,
				Msg:  err.Error(),
			}
		}
		return serializer.Response{
			Code: 200,
			Data: bscRes,
		}
	}
	bscRes, err := bl.FindNativeTransactionRecord(address)
	if err != nil {
		return serializer.Response{
			Code: 500,
			Msg:  err.Error(),
		}
	}

	return serializer.Response{
		Code: 200,
		Data: bscRes,
	}
}

func (bl *BscListener) FindNativeTransactionRecord(address string) (BscRes, error) {
	blockNum := bl.GetBlockNum()
	uuid := uuid.New()
	bl.ntManager.QueryNativeTxRecord(uuid, address, blockNum)
	res, err := bl.ntManager.WaitCall(uuid)
	if err != nil {
		return BscRes{}, err
	}
	bscRes := res.(BscRes)
	bnbRecord := make([]Result, 0)

	if len(bscRes.Result) == 0 {
		bscRes.Result = make([]Result, 0)
		return bscRes, nil
	}

	for i, result := range bscRes.Result {
		if bscRes.Result[i].Input == "0x" {
			bnbRecord = append(bnbRecord, bscRes.Result[i])
			continue
		}
		methodId := result.Input[0:10]
		switch methodId {
		case hexutil.Encode(GetTxMethodName("swapExactTokensForETHSupportingFeeOnTransferTokens(uint256,uint256,address[],address,uint256)")):
			height, err := strconv.ParseInt(bscRes.Result[i].BlockNumber, 10, 64)
			if err != nil {
				break
			}

			query := ethereum.FilterQuery{
				FromBlock: big.NewInt(height),
				ToBlock:   big.NewInt(height),
			}
			sub, err := bl.ec.FilterLogs(context.Background(), query)
			if err != nil {
				break
			}
			for _, logEvent := range sub {
				if logEvent.Topics[0].String() == EventSignHash(WITHRAWALTOPIC) {
					bscRes.Result[i].Type = "Receive"
					value := new(big.Int)
					value.SetString(strings.Split(hexutil.Encode(logEvent.Data), "0x")[1], 16)

					bscRes.Result[i].Value = value.String()
					bnbRecord = append(bnbRecord, bscRes.Result[i])
					break
				}
			}
		case hexutil.Encode(GetTxMethodName("swapExactETHForTokens(uint256,address[],address,uint256)")):
			bscRes.Result[i].Type = "Send"
			bnbRecord = append(bnbRecord, bscRes.Result[i])
		}

	}
	bscRes.Result = bnbRecord
	cacheData, _ := json.Marshal(bscRes)
	bl.rc.Set(address+nativeTxRecordSuffix, string(cacheData), duration)
	return bscRes, nil
}

func (bl *BscListener) FindFindERC20TxRecord(contractAddr, address string) (BscRes, error) {
	blockNum := bl.GetBlockNum()
	uuid := uuid.New()
	bl.etManager.QueryERC20TxRecord(uuid, contractAddr, address, blockNum)
	res, err := bl.etManager.WaitCall(uuid)
	if err != nil {
		return BscRes{}, err
	}
	bscRes := res.(BscRes)
	if len(bscRes.Result) == 0 {
		bscRes.Result = make([]Result, 0)
		return bscRes, nil
	}
	cacheData, _ := json.Marshal(bscRes)
	bl.rc.Set(address+contractAddr+erc20TxRecordSuffix, string(cacheData), duration)
	return bscRes, nil
}

func getNativeUrl(blockNumber uint64, address string) string {
	return fmt.Sprintf("%s?module=account&action=txlist&address=%s&startblock=%d&endblock=%d&offset=10000&page=1&sort=desc&apikey=%s", constants.BSCSCAN_API, address, blockNumber-201600, blockNumber, constants.BSC_API_KEY)
}

func getERC20url(contractAddr, addr string, blockNumber uint64) string {
	return fmt.Sprintf("%s?module=account&action=tokentx&address=%s&startblock=%d&endblock=%d&offset=10000&page=1&sort=desc&apikey=%s&contractaddress=%s", constants.BSCSCAN_API, addr, blockNumber-201600, blockNumber, constants.BSC_API_KEY, contractAddr)
}

func queryERC20TxRecord(contractAddr, address string, blockNum uint64) (BscRes, error) {
	bscRes := BscRes{Result: make([]Result, 0)}
	client := resty.New()
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		Get(getERC20url(contractAddr, address, blockNum))
	if err != nil {
		return bscRes, err
	}
	json.Unmarshal(resp.Body(), &bscRes)
	return bscRes, nil
}
