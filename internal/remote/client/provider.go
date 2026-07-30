package client

import "ttt/internal/domain/store"

// BindProvider fills the aggregate store's slots with remote-backed
// implementations, mirroring boltkv.BindProvider. Unlike the local backend
// it needs no Runtime: remote stores carry no bucket names and no DB handle,
// so Runtime.DB and Runtime.StoreProvider stay nil in client mode.
func BindProvider(st *store.Store, c *Client) {
	st.Tasks = &Tasks{c}
	st.Entries = &Entries{c}
	st.Tracker = &Tracker{c}
	st.Notes = &Notes{c}
}
