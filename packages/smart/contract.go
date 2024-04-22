package smart

import (
	"strings"

	"github.com/IBAX-io/needle/compiler"

	"github.com/IBAX-io/needle/vm"

	"github.com/IBAX-io/go-ibax/packages/consts"
	"github.com/IBAX-io/go-ibax/packages/storage/sqldb"
)

// Contract contains the information about the contract.
type Contract struct {
	Name          string
	Called        uint32
	FreeRequest   bool
	TxGovAccount  int64   // state wallet
	Rate          float64 // money rate
	TableAccounts string
	StackCont     []any // Stack of called contracts
	Extend        map[string]any
	Block         *compiler.CodeBlock
}

func (c *Contract) Info() *compiler.ContractInfo {
	return c.Block.GetContractInfo()
}

// LoadContracts reads and compiles contracts from smart_contracts tables
func LoadContracts() error {
	contract := &sqldb.Contract{}
	count, err := contract.Count(nil)
	if err != nil {
		return logErrorDB(err, "getting count of contracts")
	}

	defer vm.GetVM().FlushExtern()
	var offset int
	listCount := consts.ContractList
	for ; int64(offset) < count; offset += listCount {
		list, err := contract.GetList(offset, listCount)
		if err != nil {
			return logErrorDB(err, "getting list of contracts")
		}
		if err = loadContractList(list); err != nil {
			return err
		}
	}
	return nil
}

// LoadContract reads and compiles contract of new state
func LoadContract(transaction *sqldb.DbTransaction, ecosystem int64) (err error) {
	contract := &sqldb.Contract{}

	// defer vm.GetVM().FlushExtern()
	list, err := contract.GetFromEcosystem(transaction, ecosystem)
	if err != nil {
		return logErrorDB(err, "selecting all contracts from ecosystem")
	}
	if err = loadContractList(list); err != nil {
		return err
	}
	return
}

func VMGetContract(vm *vm.VM, name string, state uint32) *Contract {
	if len(name) == 0 {
		return nil
	}
	name = compiler.StateName(state, name)
	obj, ok := vm.Objects[name]

	if ok && obj.IsCodeBlockContract() {
		return &Contract{Name: name, Block: obj.GetCodeBlock()}
	}
	return nil
}

func VMGetContractByID(vm *vm.VM, id int32) *Contract {
	var tableID int64
	if id > consts.ShiftContractID {
		tableID = int64(id - consts.ShiftContractID)
		id = int32(tableID + vm.ShiftContract)
	}
	idcont := id
	if len(vm.Children) <= int(idcont) {
		return nil
	}
	if vm.Children[idcont] == nil || vm.Children[idcont].Type != compiler.CodeBlockContract {
		return nil
	}
	if tableID > 0 && vm.Children[idcont].GetContractInfo().Owner.TableId != tableID {
		return nil
	}
	return &Contract{
		Name:  vm.Children[idcont].GetContractInfo().Name,
		Block: vm.Children[idcont],
	}
}

// GetContract returns true if the contract exists in smartVM
func GetContract(name string, state uint32) *Contract {
	return VMGetContract(vm.GetVM(), name, state)
}

// GetUsedContracts returns the list of contracts which are called from the specified contract
func GetUsedContracts(name string, state uint32, full bool) []string {
	return vmGetUsedContracts(vm.GetVM(), name, state, full)
}

// GetContractByID returns true if the contract exists
func GetContractByID(id int32) *Contract {
	return VMGetContractByID(vm.GetVM(), id)
}

// GetFunc returns the block of the specified function in the contract
func (contract *Contract) GetFunc(name string) *compiler.CodeBlock {
	if block, ok := (*contract).Block.Objects[name]; ok && block.IsCodeBlockFunction() {
		return block.GetCodeBlock()
	}
	return nil
}

func loadContractList(list []sqldb.Contract) error {
	if vm.GetVM().ShiftContract == 0 {
		err := LoadSysFuncs(vm.GetVM(), 1)
		if err != nil {
			return err
		}
		vm.GetVM().ShiftContract = int64(len(vm.GetVM().Children) - 1)
	}

	for _, item := range list {
		clist, err := compiler.ContractsList(item.Value)
		if err != nil {
			return err
		}
		owner := compiler.OwnerInfo{
			StateId:  uint32(item.EcosystemID),
			Active:   false,
			TableId:  item.ID,
			WalletId: item.WalletID,
			TokenId:  item.TokenID,
		}
		if err = vm.GetVM().Compile([]rune(item.Value), &compiler.CompConfig{
			Owner:     &owner,
			IgnoreObj: vm.GetVM().Extern,
		}); err != nil {
			logErrorValue(err, consts.EvalError, "Load Contract", strings.Join(clist, `,`))
			return err
		}
	}
	return nil
}

func vmGetUsedContracts(vm *vm.VM, name string, state uint32, full bool) []string {
	contract := VMGetContract(vm, name, state)
	if contract == nil || contract.Info().Used == nil {
		return nil
	}
	ret := make([]string, 0)
	used := make(map[string]bool)
	for key := range contract.Info().Used {
		ret = append(ret, key)
		used[key] = true
		if full {
			sub := vmGetUsedContracts(vm, key, state, full)
			for _, item := range sub {
				if _, ok := used[item]; !ok {
					ret = append(ret, item)
					used[item] = true
				}
			}
		}
	}
	return ret
}
