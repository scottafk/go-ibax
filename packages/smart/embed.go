package smart

import (
	_ "embed"

	"github.com/IBAX-io/needle/compiler"
	"github.com/IBAX-io/needle/vm"
)

//go:embed extend.txt
var extendCode string

const (
	ExtendData             = `data`
	ExtendEcosystemId      = `ecosystem_id`
	ExtendType             = `type`
	ExtendTime             = `time`
	ExtendNodePosition     = `node_position`
	ExtendBlock            = `block`
	ExtendKeyId            = `key_id`
	ExtendAccountId        = `account_id`
	ExtendBlockKeyId       = `block_key_id`
	ExtendTxhash           = `txhash`
	ExtendContract         = `contract`
	ExtendBlockTime        = `block_time`
	ExtendGuestKey         = `guest_key`
	ExtendGuestAccount     = `guest_account`
	ExtendBlackHoleKey     = `black_hole_key`
	ExtendBlackHoleAccount = `black_hole_account`
	ExtendWhiteHoleKey     = `white_hole_key`
	ExtendWhiteHoleAccount = `white_hole_account`
	ExtendPreBlockDataHash = `pre_block_data_hash`
)

func Embed() []compiler.ExtendFunc {
	auto := map[string]string{"*smart.SmartContract": "sc"}
	embed := make([]compiler.ExtendFunc, 0)
	for s, a := range EmbedFuncs() {
		e := compiler.ExtendFunc{
			Name:     s,
			Func:     a,
			AutoPars: auto,
		}
		if _, ok := writeFuncs[s]; ok {
			e.CanWrite = true
		}
		embed = append(embed, e)
	}
	return embed
}

func LoadSysFuncs(vm *vm.VM, state int) error {
	return vm.Compile([]rune(extendCode), &compiler.CompConfig{Owner: &compiler.OwnerInfo{StateId: uint32(state)}})
}

var sysVars = map[string]struct{}{
	ExtendBlock:            {},
	ExtendBlockKeyId:       {},
	ExtendBlockTime:        {},
	ExtendEcosystemId:      {},
	ExtendKeyId:            {},
	ExtendAccountId:        {},
	ExtendNodePosition:     {},
	ExtendContract:         {},
	ExtendTime:             {},
	ExtendType:             {},
	ExtendTxhash:           {},
	ExtendGuestKey:         {},
	ExtendGuestAccount:     {},
	ExtendBlackHoleKey:     {},
	ExtendBlackHoleAccount: {},
	ExtendWhiteHoleKey:     {},
	ExtendWhiteHoleAccount: {},
	ExtendPreBlockDataHash: {},
}
