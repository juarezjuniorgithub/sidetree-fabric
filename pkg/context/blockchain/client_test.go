/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package blockchain

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	stmocks "github.com/trustbloc/sidetree-fabric/pkg/mocks"
)

const chID = "mychannel"

func TestNew(t *testing.T) {
	txnProvider := &stmocks.TxnServiceProvider{}
	c := New(chID, txnProvider)
	require.NotNil(t, c)
}

func TestGetClientError(t *testing.T) {
	testErr := errors.New("provider error")

	txnProvider := &stmocks.TxnServiceProvider{}
	txnProvider.ForChannelReturns(nil, testErr)

	c := New(chID, txnProvider)
	require.NotNil(t, c)

	err := c.WriteAnchor("anchor")
	require.EqualError(t, err, testErr.Error())
}

func TestWriteAnchor(t *testing.T) {
	txnService := &stmocks.TxnService{}
	txnProvider := &stmocks.TxnServiceProvider{}
	txnProvider.ForChannelReturns(txnService, nil)

	c := New(chID, txnProvider)

	err := c.WriteAnchor("anchor")
	require.Nil(t, err)
}

func TestWriteAnchorError(t *testing.T) {

	testErr := errors.New("channel error")

	txnService := &stmocks.TxnService{}
	txnService.EndorseAndCommitReturns(nil, testErr)

	txnProvider := &stmocks.TxnServiceProvider{}
	txnProvider.ForChannelReturns(txnService, nil)
	bc := New(chID, txnProvider)

	err := bc.WriteAnchor("anchor")
	require.NotNil(t, err)
	require.Contains(t, err.Error(), testErr.Error())
}

func TestClient_Read(t *testing.T) {
	require.PanicsWithValue(t, "not implemented", func() {
		txnProvider := &stmocks.TxnServiceProvider{}
		c := New(chID, txnProvider)
		c.Read(1000)
	})
}
