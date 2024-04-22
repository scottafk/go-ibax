/*---------------------------------------------------------------------------------------------
 *  Copyright (c) IBAX. All rights reserved.
 *  See LICENSE in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

package smart

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/IBAX-io/go-ibax/packages/common"
	"github.com/IBAX-io/go-ibax/packages/common/crypto"
	"github.com/IBAX-io/go-ibax/packages/conf/syspar"
	"github.com/IBAX-io/go-ibax/packages/consts"
	"github.com/IBAX-io/go-ibax/packages/converter"
	"github.com/IBAX-io/go-ibax/packages/storage/sqldb"
	qb "github.com/IBAX-io/go-ibax/packages/storage/sqldb/queryBuilder"
	"github.com/IBAX-io/go-ibax/packages/types"
	"github.com/IBAX-io/go-ibax/packages/utils"
	"github.com/IBAX-io/needle/compiler"
	"github.com/IBAX-io/needle/vm"
	"github.com/pkg/errors"

	"github.com/shopspring/decimal"
	log "github.com/sirupsen/logrus"
)

const (
	// MaxPrice is a maximal value that price function can return
	MaxPrice = 100000000000000000
)

const (
	CallDelayedContract = "@1CallDelayedContract"
	NewUserContract     = "@1NewUser"
	NewBadBlockContract = "@1NewBadBlock"
)

var builtinContract = map[string]bool{
	CallDelayedContract: true,
	NewUserContract:     true,
	NewBadBlockContract: true,
}

// SmartContract is storing smart contract data
type SmartContract struct {
	CLB             bool
	Rollback        bool
	FullAccess      bool
	SysUpdate       bool
	VM              *vm.VM
	TxSmart         *types.SmartTransaction
	TxData          map[string]any
	TxContract      *Contract
	TxFuel          int64           // The fuel of executing contract
	TxCost          int64           // Maximum cost of executing contract
	TxUsedCost      decimal.Decimal // Used cost of CPU resources
	TXBlockFuel     decimal.Decimal
	BlockHeader     *types.BlockHeader
	PreBlockHeader  *types.BlockHeader
	Loop            map[string]bool
	Hash            []byte
	Payload         []byte
	Timestamp       int64
	TxSignature     []byte
	TxSize          int64
	Size            common.StorageSize
	PublicKeys      [][]byte
	DbTransaction   *sqldb.DbTransaction
	Rand            *rand.Rand
	FlushRollback   []*FlushInfo
	Notifications   types.Notifications
	GenBlock        bool
	TimeLimit       int64
	Key             *sqldb.Key
	RollBackTx      []*types.RollbackTx
	multiPays       multiPays
	taxes           bool
	Penalty         bool
	TokenEcosystems map[int64]any
	OutputsMap      map[sqldb.KeyUTXO][]sqldb.SpentInfo
	TxInputsMap     map[sqldb.KeyUTXO][]sqldb.SpentInfo
	TxOutputsMap    map[sqldb.KeyUTXO][]sqldb.SpentInfo
	PrevSysPar      map[string]string
	EcoParams       []sqldb.EcoParam
}

// AppendStack adds an element to the stack of contract call or removes the top element when name is empty
func (sc *SmartContract) AppendStack(fn string) error {
	if sc.isAllowStack(fn) {
		cont := sc.TxContract
		for _, item := range cont.StackCont {
			if item == fn {
				return fmt.Errorf(eContractLoop, fn)
			}
		}
		cont.StackCont = append(cont.StackCont, fn)
		sc.TxContract.Extend[vm.ExtendStack] = cont.StackCont
	}
	return nil
}

func (sc *SmartContract) PopStack(fn string) {
	if sc.isAllowStack(fn) {
		cont := sc.TxContract
		if len(cont.StackCont) > 0 {
			cont.StackCont = cont.StackCont[:len(cont.StackCont)-1]
			sc.TxContract.Extend[vm.ExtendStack] = cont.StackCont
		}
	}
}

func (sc *SmartContract) isAllowStack(fn string) bool {
	// Stack contains only contracts
	c := VMGetContract(sc.VM, fn, uint32(sc.TxSmart.EcosystemID))
	return c != nil
}

func InitVM() {
	var v []string
	for p := range sysVars {
		v = append(v, p)
	}
	vm.GetVM().SetExtendCost(getCost).SetFuncCallsDB(funcCallsDBP).AppendPreVar(v).SetExtendFunc(Embed())
}

// GetLogger is returning logger
func (sc *SmartContract) GetLogger() *log.Entry {
	var name string
	if sc.TxContract != nil {
		name = sc.TxContract.Name
	}
	return log.WithFields(log.Fields{"tx": fmt.Sprintf("%x", sc.Hash), "clb": sc.CLB, "name": name, "tx_eco": sc.TxSmart.EcosystemID})
}

func GetAllContracts() (string, error) {
	var ret []string
	for k := range vm.GetVM().Objects {
		ret = append(ret, k)
	}

	sort.Strings(ret)
	resultByte, err := json.Marshal(ret)
	result := string(resultByte)
	return result, err
}

// ActivateContract sets Active status of the contract in vm.GetVM()
func ActivateContract(tblid, state int64, active bool) {
	for i, item := range vm.GetVM().CodeBlock.Children {
		if item != nil && item.Type == compiler.CodeBlockContract {
			cinfo := item.GetContractInfo()
			if cinfo.Owner.TableId == tblid && cinfo.Owner.StateId == uint32(state) {
				vm.GetVM().Children[i].GetContractInfo().Owner.Active = active
			}
		}
	}
}

// SetContractWallet changes WalletID of the contract in vm.GetVM()
func SetContractWallet(sc *SmartContract, tblid, state int64, wallet int64) error {
	if err := validateAccess(sc, "SetContractWallet"); err != nil {
		return err
	}
	for i, item := range vm.GetVM().CodeBlock.Children {
		if cinfo := item.GetContractInfo(); cinfo != nil {
			if cinfo.Owner.TableId == tblid && cinfo.Owner.StateId == uint32(state) {
				vm.GetVM().Children[i].GetContractInfo().Owner.WalletId = wallet
			}
		}
	}
	return nil
}

func (sc *SmartContract) getExtend() map[string]any {
	var block, blockTime, blockKeyID, blockNodePosition int64
	var perBlockHash string
	if sc.BlockHeader != nil {
		block = sc.BlockHeader.BlockId
		blockKeyID = sc.BlockHeader.KeyId
		blockTime = sc.BlockHeader.Timestamp
		blockNodePosition = sc.BlockHeader.NodePosition
	}
	if sc.PreBlockHeader != nil {
		perBlockHash = hex.EncodeToString(sc.PreBlockHeader.BlockHash)
	}
	head := sc.TxSmart
	extend := map[string]any{
		ExtendType:              head.ID,
		ExtendTime:              sc.Timestamp,
		ExtendEcosystemId:       head.EcosystemID,
		ExtendNodePosition:      blockNodePosition,
		ExtendBlock:             block,
		ExtendKeyId:             sc.Key.ID,
		ExtendAccountId:         sc.Key.AccountID,
		ExtendBlockKeyId:        blockKeyID,
		vm.ExtendParentContract: ``,
		// vm.ExtendTxCost:    sc.GetContractLimit(),
		ExtendTxhash: sc.Hash,
		// vm.ExtendResult:           ``,
		vm.ExtendSc:               sc,
		ExtendContract:            sc.TxContract,
		ExtendBlockTime:           blockTime,
		vm.ExtendOriginalContract: ``,
		vm.ExtendThisContract:     ``,
		ExtendGuestKey:            consts.GuestKey,
		ExtendGuestAccount:        consts.GuestAddress,
		ExtendBlackHoleKey:        converter.HoleAddrMap[converter.BlackHoleAddr].K,
		ExtendBlackHoleAccount:    converter.HoleAddrMap[converter.BlackHoleAddr].S,
		ExtendWhiteHoleKey:        converter.HoleAddrMap[converter.WhiteHoleAddr].K,
		ExtendWhiteHoleAccount:    converter.HoleAddrMap[converter.WhiteHoleAddr].S,
		ExtendPreBlockDataHash:    perBlockHash,
		vm.ExtendGenBlock:         sc.GenBlock,
		vm.ExtendTimeLimit:        sc.TimeLimit,
	}
	//if ecost, ok := sc.TxContract.Extend[vm.ExtendTxCost]; ok {
	//	extend[vm.ExtendTxCost] = ecost.(int64)
	//} else {
	extend[vm.ExtendTxCost] = sc.GetContractLimit()
	//}
	for key, val := range sc.TxData {
		extend[key] = val
	}

	return extend
}

func PrefixName(table string) (prefix, name string) {
	name = table
	if off := strings.IndexByte(table, '_'); off > 0 && table[0] >= '0' && table[0] <= '9' {
		prefix = table[:off]
		name = table[off+1:]
	}
	return
}

func (sc *SmartContract) IsCustomTable(table string) (isCustom bool, err error) {
	prefix, name := PrefixName(table)
	if len(prefix) > 0 {
		tables := &sqldb.Table{}
		tables.SetTablePrefix(prefix)
		found, err := tables.Get(sc.DbTransaction, name)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

func (sc *SmartContract) AccessTablePerm(table, action string) (map[string]string, error) {
	var (
		err             error
		tablePermission map[string]string
	)
	logger := sc.GetLogger()
	isRead := action == `read`
	if qb.GetTableName(sc.TxSmart.EcosystemID, table) == `1_parameters` || qb.GetTableName(sc.TxSmart.EcosystemID, table) == `1_app_params` {
		if isRead || sc.TxSmart.KeyID == converter.StrToInt64(EcosysParam(sc, `founder_account`)) {
			return tablePermission, nil
		}
		logger.WithFields(log.Fields{"type": consts.AccessDenied}).Error("Access denied")
		return tablePermission, errAccessDenied
	}

	if isCustom, err := sc.IsCustomTable(table); err != nil {
		logger.WithFields(log.Fields{"table": table, "error": err, "type": consts.DBError}).Error("checking custom table")
		return tablePermission, err
	} else if !isCustom {
		if isRead {
			return tablePermission, nil
		}
		return tablePermission, fmt.Errorf(eNotCustomTable, table)
	}

	prefix, name := PrefixName(table)
	tables := &sqldb.Table{}
	tables.SetTablePrefix(prefix)
	tablePermission, err = tables.GetPermissions(sc.DbTransaction, name, "")
	if err != nil {
		logger.WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("getting table permissions")
		return tablePermission, err
	}
	if len(tablePermission[action]) > 0 {
		ret, err := sc.EvalIf(tablePermission[action])
		if err != nil {
			logger.WithFields(log.Fields{"table": table, "action": action, "permissions": tablePermission[action], "error": err, "type": consts.EvalError}).Error("evaluating table permissions for action")
			return tablePermission, err
		}
		if !ret {
			logger.WithFields(log.Fields{"table": table, "action": action, "permissions": tablePermission[action], "type": consts.EvalError}).Error("access denied")
			return tablePermission, fmt.Errorf("table: %w", errAccessDenied)
		}
	}
	return tablePermission, nil
}

// AccessTable checks the access right to the table
func (sc *SmartContract) AccessTable(table, action string) error {
	if sc.FullAccess {
		return nil
	}
	_, err := sc.AccessTablePerm(table, action)
	return err
}

func getPermColumns(input string) (perm permColumn, err error) {
	if strings.HasPrefix(input, `{`) {
		err = unmarshalJSON([]byte(input), &perm, `on perm columns`)
	} else {
		perm.Update = input
	}
	return
}

// AccessColumns checks access rights to the columns
func (sc *SmartContract) AccessColumns(table string, columns *[]string, update bool) error {
	logger := sc.GetLogger()
	if sc.FullAccess {
		return nil
	}
	if qb.GetTableName(sc.TxSmart.EcosystemID, table) == `1_parameters` || qb.GetTableName(sc.TxSmart.EcosystemID, table) == `1_app_params` {
		if update {
			if sc.TxSmart.KeyID == converter.StrToInt64(EcosysParam(sc, `founder_account`)) {
				return nil
			}
			log.WithFields(log.Fields{"txSmart.KeyId": sc.TxSmart.KeyID}).Error("ACCESS DENIED")
			return errAccessDenied
		}
		return nil
	}
	prefix, name := PrefixName(table)
	tables := &sqldb.Table{}
	tables.SetTablePrefix(prefix)
	found, err := tables.Get(sc.DbTransaction, name)
	if err != nil {
		logger.WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("getting table columns")
		return err
	}
	if !found {
		if !update {
			return nil
		}
		return fmt.Errorf(eTableNotFound, table)
	}
	var cols map[string]string
	err = json.Unmarshal([]byte(tables.Columns), &cols)
	if err != nil {
		logger.WithFields(log.Fields{"type": consts.JSONUnmarshallError, "error": err}).Error("getting table columns")
		return err
	}
	colNames := make([]string, 0, len(*columns))
	for _, col := range *columns {
		if col == `*` {
			for column := range cols {
				colNames = append(colNames, column)
			}
			continue
		}
		colNames = append(colNames, col)
	}

	colList := make([]string, len(colNames))
	for i, col := range colNames {
		colname := converter.Sanitize(col, `->`)
		if strings.Contains(colname, `->`) {
			colname = colname[:strings.Index(colname, `->`)]
		}
		colList[i] = colname
	}
	checked := make(map[string]bool)
	var notaccess bool
	for i, name := range colList {
		if status, ok := checked[name]; ok {
			if !status {
				colList[i] = ``
			}
			continue
		}
		cond := cols[name]
		if len(cond) > 0 {
			perm, err := getPermColumns(cond)
			if err != nil {
				logger.WithFields(log.Fields{"type": consts.InvalidObject, "error": err}).Error("getting access columns")
				return err
			}
			if update {
				cond = perm.Update
			} else {
				cond = perm.Read
			}
			if len(cond) > 0 {
				ret, err := sc.EvalIf(cond)
				if err != nil {
					logger.WithFields(log.Fields{
						"condition": cond, "column": name,
						"type": consts.EvalError,
					}).Error("evaluating condition")
					return err
				}
				checked[name] = ret
				if !ret {
					if update {
						logger.WithFields(log.Fields{"table": table, "column": name, "condition": cond, "type": consts.EvalError}).Error("access denied")
						return fmt.Errorf("column: %w", errAccessDenied)
					}
					colList[i] = ``
					notaccess = true
				}
			}
		}
	}
	if !update && notaccess {
		retColumn := make([]string, 0)
		for i, val := range colList {
			if val != `` {
				retColumn = append(retColumn, colNames[i])
			}
		}
		if len(retColumn) == 0 {
			return errAccessDenied
		}
		*columns = retColumn
	}
	return nil
}

func (sc *SmartContract) CheckAccess(tableName, columns string, ecosystem int64) (table string, perm map[string]string,
	cols string, err error,
) {
	var collist []string

	table = converter.ParseTable(tableName, ecosystem)
	collist, err = qb.GetColumns(columns)
	if err != nil {
		return
	}
	if !syspar.IsPrivateBlockchain() {
		cols = PrepareColumns(collist)
		return
	}
	perm, err = sc.AccessTablePerm(table, `read`)
	if err != nil {
		return
	}
	if err = sc.AccessColumns(table, &collist, false); err != nil {
		return
	}
	cols = PrepareColumns(collist)
	return
}

// AccessRights checks the access right by executing the condition value
func (sc *SmartContract) AccessRights(condition string, iscondition bool) error {
	sp := &sqldb.StateParameter{}
	prefix := converter.Int64ToStr(sc.TxSmart.EcosystemID)

	sp.SetTablePrefix(prefix)
	_, err := sp.Get(sc.DbTransaction, condition)
	if err != nil {
		return err
	}
	conditions := sp.Value
	if iscondition {
		conditions = sp.Conditions
	}
	if len(conditions) > 0 {
		ret, err := sc.EvalIf(conditions)
		if err != nil {
			return err
		}
		if !ret {
			return errAccessDenied
		}
	} else {
		return fmt.Errorf(eNotCondition, condition)
	}
	return nil
}

// EvalIf counts and returns the logical value of the specified expression
func (sc *SmartContract) EvalIf(conditions string) (bool, error) {
	sc.getExtend()[vm.ExtendTxCost] = syspar.GetMaxCost()
	return sc.VM.EvalIf(conditions, uint32(sc.TxSmart.EcosystemID), sc.getExtend())
}

// GetContractLimit returns the default maximal cost of contract
func (sc *SmartContract) GetContractLimit() (ret int64) {
	// default maximum cost of F
	if len(sc.TxSmart.MaxSum) > 0 {
		sc.TxCost = converter.StrToInt64(sc.TxSmart.MaxSum)
	} else {
		sc.TxCost = syspar.GetMaxCost()
	}
	return sc.TxCost
}

func (sc *SmartContract) GetSignedBy(public []byte) (int64, error) {
	signedBy := sc.TxSmart.KeyID
	if sc.TxSmart.SignedBy != 0 {
		var isNode bool
		signedBy = sc.TxSmart.SignedBy
		if syspar.IsCandidateNodeMode() {
			return signedBy, nil
		}
		honorNodes := syspar.GetNodes()
		delay := sqldb.DelayedContract{}
		if ok, _ := delay.GetByContract(sc.DbTransaction, sc.TxContract.Name); !ok && !builtinContract[sc.TxContract.Name] {
			return 0, fmt.Errorf("%w: %v", errDelayedContract, sc.TxContract.Name)
		}
		if len(honorNodes) > 0 {
			for _, node := range honorNodes {
				if crypto.Address(node.PublicKey) == signedBy {
					isNode = true
					break
				}
			}
		} else {
			isNode = crypto.Address(syspar.GetNodePubKey()) == signedBy
		}

		if sc.TxContract.Name == NewUserContract && !isNode {
			return signedBy, nil
		}
		if !isNode {
			return 0, errDelayedContract
		}
	} else if len(public) > 0 && sc.TxSmart.KeyID != crypto.Address(public) {
		return 0, errDiffKeys
	}
	return signedBy, nil
}

// CallContract calls the contract functions according to the specified flags
func (sc *SmartContract) CallContract(point string) (string, error) {
	var (
		result string
		err    error
	)
	logger := sc.GetLogger()

	retError := func(err error) (string, error) {
		eText := err.Error()
		if !strings.HasPrefix(eText, `{`) && !errors.Is(err, vm.ErrVMTimeLimit) {
			err = vm.VMError{Type: "panic", Err: eText}
		}
		return ``, err
	}

	if err = sc.checkTxSign(); err != nil {
		return ``, err
	}

	needPayment := sc.needPayment()
	if needPayment {
		err = sc.prepareMultiPay()
		if err != nil {
			logger.WithError(err).Error("prepare multi")
			return ``, err
		}
	}

	sc.TxContract.Extend = sc.getExtend()
	if err = sc.AppendStack(sc.TxContract.Name); err != nil {
		logger.WithFields(log.Fields{"type": consts.ContractError, "error": err}).Error("loop in contract")
		return retError(err)
	}
	sc.VM = vm.GetVM()

	ctrctExtend := sc.TxContract.Extend
	before := ctrctExtend[vm.ExtendTxCost].(int64)
	txSizeFuel := syspar.GetSizeFuel() * sc.TxSize / 1024
	ctrctExtend[vm.ExtendTxCost] = ctrctExtend[vm.ExtendTxCost].(int64) - txSizeFuel

	_, nameContract := converter.ParseName(sc.TxContract.Name)
	ctrctExtend[vm.ExtendOriginalContract] = nameContract
	ctrctExtend[vm.ExtendThisContract] = nameContract

	methods := []string{`conditions`, `action`}
	err = vm.RunContractById(sc.VM, int32(sc.TxSmart.ID), methods, sc.TxContract.Extend)
	if ctrctExtend[vm.ExtendTxCost].(int64) < 0 {
		sc.TxFuel = before
	} else {
		sc.TxFuel = before - ctrctExtend[vm.ExtendTxCost].(int64)
	}
	sc.TxUsedCost = decimal.New(sc.TxFuel, 0)

	if err == nil {
		if ctrctExtend[vm.ExtendResult] != nil {
			result = fmt.Sprint(ctrctExtend[vm.ExtendResult])
			if !utf8.ValidString(result) {
				result, err = retError(errNotValidUTF)
			}
			if len(result) > 255 {
				result = result[:255] + `...`
			}
		}
	}
lp:
	if err != nil {
		sc.RollBackTx = nil
		sc.DbTransaction.BinLogSql = nil
		if errReset := sc.DbTransaction.ResetSavepoint(point); errReset != nil {
			return retError(errors.Wrap(err, errReset.Error()))
		}
		if needPayment {
			if errPay := sc.payContract(true); errPay != nil {
				sc.RollBackTx = nil
				sc.DbTransaction.BinLogSql = nil
				if errRollsp := sc.DbTransaction.RollbackSavepoint(point); errRollsp != nil {
					return retError(errors.Wrap(err, errRollsp.Error()))
				}
				return errors.Wrap(err, errPay.Error()).Error(), nil
			}
			return err.Error(), nil
		}
		return retError(err)
	}

	if needPayment {
		if errPay := sc.payContract(false); errPay != nil {
			err = errPay
			goto lp
		}
	}
	return result, nil
}

func (sc *SmartContract) checkTxSign() error {
	var public []byte
	if len(sc.TxSmart.PublicKey) > 0 && string(sc.TxSmart.PublicKey) != `null` {
		public = sc.TxSmart.PublicKey
	}
	signedBy, err := sc.GetSignedBy(public)
	if err != nil {
		return err
	}

	isFound, err := sc.Key.SetTablePrefix(sc.TxSmart.EcosystemID).Get(sc.DbTransaction, signedBy)
	if err != nil {
		sc.GetLogger().WithFields(log.Fields{"type": consts.DBError, "error": err}).Error("getting wallet")
		return err
	}

	if !isFound {
		err = fmt.Errorf(eEcoKeyNotFound, converter.AddressToString(signedBy), sc.TxSmart.EcosystemID)
		sc.GetLogger().WithFields(log.Fields{"type": consts.ContractError, "error": err}).Error("looking for keyid")
		return err
	}
	if sc.Key.Disable() {
		err = fmt.Errorf(eEcoKeyDisable, converter.AddressToString(signedBy), sc.TxSmart.EcosystemID)
		sc.GetLogger().WithFields(log.Fields{"type": consts.ContractError, "error": err}).Error("disable keyid")
		return err
	}
	if len(sc.Key.PublicKey) > 0 {
		public = sc.Key.PublicKey
	}
	if len(public) == 0 {
		sc.GetLogger().WithFields(log.Fields{"type": consts.EmptyObject}).Error("empty public key")
		return errEmptyPublicKey
	}
	sc.PublicKeys = append(sc.PublicKeys, public)

	var CheckSignResult bool

	CheckSignResult, err = utils.CheckSign(sc.PublicKeys, sc.Hash, sc.TxSignature, false)
	if err != nil {
		sc.GetLogger().WithFields(log.Fields{"type": consts.CryptoError, "error": err}).Error("checking tx data sign")
		return err
	}
	if !CheckSignResult {
		sc.GetLogger().WithFields(log.Fields{"type": consts.InvalidObject}).Error("incorrect sign")
		return errIncorrectSign
	}
	return nil
}
