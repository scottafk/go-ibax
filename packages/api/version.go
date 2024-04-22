/*---------------------------------------------------------------------------------------------
 *  Copyright (c) IBAX. All rights reserved.
 *  See LICENSE in the project root for license information.
 *--------------------------------------------------------------------------------------------*/

package api

import (
	"net/http"

	"github.com/IBAX-io/go-ibax/packages/consts"
	"github.com/IBAX-io/go-ibax/packages/service/node"
)

func getVersionHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, consts.Version()+" "+node.NodePauseType().String())
}
