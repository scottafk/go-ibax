/*---------------------------------------------------------------------------------------------
 *  Copyright (c) IBAX. All rights reserved.
 *  See LICENSE in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

package api

import (
	"net/http"

	"github.com/IBAX-io/go-ibax/packages/storage/sqldb"
	"github.com/IBAX-io/needle/vm"

	"github.com/IBAX-io/go-ibax/packages/consts"
	"github.com/IBAX-io/go-ibax/packages/converter"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
)

type contractField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Optional bool   `json:"optional"`
}

type getContractResult struct {
	ID         uint32          `json:"id"`
	StateID    uint32          `json:"state"`
	TableID    string          `json:"tableid"`
	WalletID   string          `json:"walletid"`
	TokenID    string          `json:"tokenid"`
	Address    string          `json:"address"`
	Fields     []contractField `json:"fields"`
	Name       string          `json:"name"`
	AppId      uint32          `json:"app_id"`
	Ecosystem  uint32          `json:"ecosystem"`
	Conditions string          `json:"conditions"`
}

func getContractInfoHandler(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)
	logger := getLogger(r)

	contract := getContract(r, params["name"])
	if contract == nil {
		logger.WithFields(log.Fields{"type": consts.ContractError, "contract_name": params["name"]}).Debug("contract name")
		errorResponse(w, errContract.Errorf(params["name"]))
		return
	}

	var result getContractResult
	info := getContractInfo(contract)
	con := &sqldb.Contract{}
	exits, err := con.Get(info.Owner.TableId)
	if err != nil {
		logger.WithFields(log.Fields{"type": consts.DBError, "error": err, "contract_id": info.Owner.TableId}).Error("get contract")
		errorResponse(w, errQuery)
		return
	}
	if !exits {
		logger.WithFields(log.Fields{"type": consts.ContractError, "contract id": info.Owner.TableId}).Debug("get contract")
		errorResponse(w, errContract.Errorf(params["name"]))
		return
	}
	fields := make([]contractField, 0)
	result = getContractResult{
		ID:         uint32(info.Owner.TableId + consts.ShiftContractID),
		TableID:    converter.Int64ToStr(info.Owner.TableId),
		Name:       info.Name,
		StateID:    info.Owner.StateId,
		WalletID:   converter.Int64ToStr(info.Owner.WalletId),
		TokenID:    converter.Int64ToStr(info.Owner.TokenId),
		Address:    converter.AddressToString(info.Owner.WalletId),
		Ecosystem:  uint32(con.EcosystemID),
		AppId:      uint32(con.AppID),
		Conditions: con.Conditions,
	}

	if info.Field != nil {
		for _, fitem := range *info.Field {
			fields = append(fields, contractField{
				Name:     fitem.Name,
				Type:     fitem.Type.ToString(),
				Optional: fitem.ContainsTag(vm.TagOptional),
			})
		}
	}
	result.Fields = fields

	jsonResponse(w, result)
}
