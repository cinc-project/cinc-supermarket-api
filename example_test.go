package supermarket_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	supermarket "github.com/cinc-project/cinc-supermarket-api"
)

func ExampleCompareVersions() {
	fmt.Println(supermarket.CompareVersions("1.10.0", "1.2.0"))
	fmt.Println(supermarket.CompareVersions("1.0", "1.0.0"))
	fmt.Println(supermarket.CompareVersions("2.0.0-rc.1", "2.0.0"))
	// Output:
	// 1
	// 0
	// -1
}

func ExampleLatestVersion() {
	fmt.Println(supermarket.LatestVersion([]string{"1.2.0", "1.10.0", "1.9.3", "2.0.0-beta"}))
	// Output: 2.0.0-beta
}

func ExampleVersionFromURL() {
	fmt.Println(supermarket.VersionFromURL("https://supermarket.chef.io/api/v1/cookbooks/nginx/versions/12_0_4"))
	fmt.Println(supermarket.VersionFromURL("https://supermarket.chef.io/api/v1/cookbooks/nginx/versions/12_0_4/download"))
	fmt.Printf("%q\n", supermarket.VersionFromURL("https://supermarket.chef.io/api/v1/cookbooks/nginx"))
	// Output:
	// 12.0.4
	// 12.0.4
	// ""
}

// Read endpoints are anonymous, so a zero Config talks to the public
// Supermarket without credentials.
func ExampleCookbooksService_Get() {
	c, err := supermarket.NewClient(supermarket.Config{})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cb, _, err := c.Cookbooks.Get(ctx, "apache2")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(cb.Name, cb.LatestVersion)
}

// List returns one page at a time. Page.HasMore reports whether another page
// exists and Page.NextStart is the offset to request it with; a page with no
// items always ends the loop, even if Total claims more.
func Example_pagination() {
	c, err := supermarket.NewClient(supermarket.Config{})
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	opts := supermarket.ListOptions{Items: 100, Order: "most_downloaded"}
	for {
		page, _, err := c.Cookbooks.List(ctx, opts)
		if err != nil {
			log.Fatal(err)
		}
		for _, cb := range page.Items {
			fmt.Println(cb.Name)
		}
		if !page.HasMore() {
			break
		}
		opts.Start = page.NextStart()
	}
}

// Supermarket reports a missing resource as 400 with error_code NOT_FOUND
// rather than 404. Match on the sentinel, not the status code.
func Example_notFound() {
	c, err := supermarket.NewClient(supermarket.Config{})
	if err != nil {
		log.Fatal(err)
	}

	_, _, err = c.Cookbooks.Get(context.Background(), "no-such-cookbook")
	switch {
	case errors.Is(err, supermarket.ErrNotFound):
		fmt.Println("cookbook does not exist")
	case err != nil:
		log.Fatal(err)
	}
}

// Write endpoints are signed with the RSA key registered on the user's
// Supermarket profile. A client without Username and Key returns
// ErrUnauthenticatedWrite from them without contacting the server.
func ExampleLoadKeyFile() {
	key, err := supermarket.LoadKeyFile("/home/me/.chef/me.pem")
	if err != nil {
		log.Fatal(err)
	}
	c, err := supermarket.NewClient(supermarket.Config{
		Username: "me",
		Key:      key,
	})
	if err != nil {
		log.Fatal(err)
	}

	if _, _, err := c.Cookbooks.DeleteVersion(context.Background(), "my-cookbook", "0.1.0"); err != nil {
		log.Fatal(err)
	}
}

// A private Supermarket whose certificate is issued by an internal CA is
// reached by trusting that CA, which keeps certificate verification on.
func ExampleWithRootCAs() {
	pem, err := os.ReadFile("/etc/ssl/internal-ca.pem")
	if err != nil {
		log.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		log.Fatal("no certificates found in internal-ca.pem")
	}

	c, err := supermarket.NewClient(
		supermarket.Config{BaseURL: "https://supermarket.example.internal"},
		supermarket.WithRootCAs(pool),
	)
	if err != nil {
		log.Fatal(err)
	}

	cb, _, err := c.Cookbooks.Get(context.Background(), "apache2")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(cb.Name, cb.LatestVersion)
}

// GetStream hands back the raw /universe body so it can be decoded one
// cookbook at a time instead of holding the whole graph in memory. Bound the
// download with ctx: the client's total-transaction timeout does not apply to
// streams.
func ExampleUniverseService_GetStream() {
	c, err := supermarket.NewClient(supermarket.Config{})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	body, _, err := c.Universe.GetStream(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer body.Close()

	dec := json.NewDecoder(body)
	if _, err := dec.Token(); err != nil { // opening '{'
		log.Fatal(err)
	}
	for dec.More() {
		tok, err := dec.Token() // cookbook name
		if err != nil {
			log.Fatal(err)
		}
		var versions map[string]supermarket.UniverseVersion
		if err := dec.Decode(&versions); err != nil {
			log.Fatal(err)
		}
		fmt.Println(tok.(string), len(versions))
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		log.Fatal(err)
	}
}
