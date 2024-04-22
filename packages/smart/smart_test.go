/*---------------------------------------------------------------------------------------------
 *  Copyright (c) IBAX. All rights reserved.
 *  See LICENSE in the project root for license information.
 *--------------------------------------------------------------------------------------------*/
package smart

import (
	"testing"

	"github.com/IBAX-io/needle/compiler"
	"github.com/IBAX-io/needle/vm"

	"github.com/stretchr/testify/require"
)

type TestSmart struct {
	Input  string
	Output string
}

func TestNewContract(t *testing.T) {
	test := []TestSmart{
		{`contract NewCitizen {
			data {
				Public bytes
				MyVal  string
			}
			 func conditions {
				Println( "Front")
				//$tmp = "Test string"
//				Println("NewCitizen Front", $tmp, $key_id, $ecosystem_id, $PublicKey )
			}
			settings {
				abc = 123
			}
			func action {
//				Println("NewCitizen Main", $tmp, $type, $key_id )
//				DBInsert(Sprintf( "%d_citizens", $ecosystem_id), "public_key,block_id", $PublicKey, $block)
			}
}			
		`, ``},
	}
	owner := compiler.OwnerInfo{
		StateID:  1,
		Active:   false,
		TableID:  1,
		WalletID: 0,
		TokenID:  0,
	}
	InitVM()
	for _, item := range test {
		if err := vm.GetVM().Compile([]rune(item.Input), &owner); err != nil {
			t.Error(err)
		}
	}
	cnt := GetContract(`NewCitizen`, 1)
	cfunc := cnt.GetFunc(`conditions`)
	_, err := vm.Run(vm.GetVM(), cfunc, map[string]any{})
	if err != nil {
		t.Error(err)
	}
}

func TestCheckAppend(t *testing.T) {
	appendTestContract := `contract AppendTest {
		action {
			var list array
			list = Append(list, "naw_value")
			Println(list)
		}
	}`

	owner := compiler.OwnerInfo{
		StateID:  1,
		Active:   false,
		TableID:  1,
		WalletID: 0,
		TokenID:  0,
	}

	require.NoError(t, vm.GetVM().Compile([]rune(appendTestContract), &owner))

	cnt := GetContract("AppendTest", 1)
	cfunc := cnt.GetFunc("action")

	_, err := vm.Run(vm.GetVM(), cfunc, map[string]any{})
	require.NoError(t, err)
}
