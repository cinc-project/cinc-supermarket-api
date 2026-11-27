// Package supermarket is a Go client for the Chef Supermarket API.
//
// Read endpoints (cookbooks, search, tools, users, /universe, health) are
// anonymous; build a Client with no credentials:
//
//	c, err := supermarket.NewClient(supermarket.Config{})
//	if err != nil {
//		return err
//	}
//	cb, _, err := c.Cookbooks.Get(ctx, "apache2")
//	if err != nil {
//		return err // errors.Is(err, supermarket.ErrNotFound) for a missing cookbook
//	}
//
// Write endpoints (share, delete) use the Chef mixlib-authentication
// signed-header protocol (version 1.1, SHA-1 — the version the public
// Supermarket accepts). The credentials are the Supermarket username and the RSA
// private key whose public half is registered on that user's
// Supermarket profile:
//
//	key, err := supermarket.LoadKeyFile("/home/me/.chef/me.pem")
//	if err != nil {
//		return err
//	}
//	c, err := supermarket.NewClient(supermarket.Config{Username: "me", Key: key})
//	if err != nil {
//		return err
//	}
//	if _, _, err := c.Cookbooks.Share(ctx, "apache2", "Web Servers", tarball); err != nil {
//		return err
//	}
//
// All list and search endpoints share a paginated envelope; the
// generic Page[T] type carries the items along with Start/Items/Total
// for cursoring. Loop while Page.HasMore, passing Page.NextStart as the
// next Start.
//
// The examples in this package show each of these in full.
package supermarket
