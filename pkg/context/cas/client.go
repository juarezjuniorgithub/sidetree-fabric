/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package cas

import (
	"github.com/trustbloc/fabric-peer-ext/pkg/collections/offledger/dcas/client"
)

const (
	sidetreeTxnCC = "sidetreetxn_cc"
	collection    = "dcas"
)

type dcasClientProvider interface {
	ForChannel(channelID string) (client.DCAS, error)
}

// Client implements client for accessing the underlying content addressable storage
type Client struct {
	dcasProvider dcasClientProvider
	channelID    string
}

// New returns a new CAS client
func New(channelID string, dcasProvider dcasClientProvider) *Client {
	return &Client{
		channelID:    channelID,
		dcasProvider: dcasProvider,
	}
}

// Write writes the given content to content addressable storage
// returns the SHA256 hash in base64url encoding which represents the address of the content.
func (c *Client) Write(content []byte) (string, error) {
	dcasClient, err := c.dcasProvider.ForChannel(c.channelID)
	if err != nil {
		return "", err
	}
	return dcasClient.Put(sidetreeTxnCC, collection, content)
}

// Read reads the content at the given address from content addressable storage
// returns the content of the given address.
func (c *Client) Read(address string) ([]byte, error) {
	dcasClient, err := c.dcasProvider.ForChannel(c.channelID)
	if err != nil {
		return nil, err
	}
	return dcasClient.Get(sidetreeTxnCC, collection, address)
}
