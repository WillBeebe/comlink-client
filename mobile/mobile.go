// Package mobile is Comlink's native in-process API for Apple platforms.
package mobile

import "comlink/internal/handset"

type Client struct{ native *handset.Native }

func NewClient(profile, roots, endpoint string) *Client {
	return &Client{handset.NewNative(profile, roots, endpoint)}
}
func (c *Client) Open() (string, error)     { return c.native.Open() }
func (c *Client) Close()                    { c.native.Close() }
func (c *Client) Contacts() (string, error) { return c.native.Contacts() }
func (c *Client) SaveContact(name, number string, approved bool) error {
	return c.native.SaveContact(name, number, approved)
}
func (c *Client) Dial(number string) (string, error) { return c.native.Dial(number) }
func (c *Client) Act(action, text string) error      { return c.native.Act(action, text) }
func (c *Client) NextEvent() (string, error)         { return c.native.NextEvent() }
