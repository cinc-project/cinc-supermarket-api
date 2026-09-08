package supermarket

import (
	"context"
	"net/http"
	"net/url"
)

// User is the profile representation returned by GET /users/:username.
type User struct {
	Username  string        `json:"username"`
	Name      string        `json:"name"`
	Company   string        `json:"company"`
	GitHub    []string      `json:"github"`
	Twitter   string        `json:"twitter"`
	Slack     string        `json:"slack"`
	Cookbooks UserCookbooks `json:"cookbooks"`
	Tools     UserTools     `json:"tools"`
}

// UserCookbooks groups the cookbook relationships on a user profile.
//
// Supermarket serializes each relationship as a JSON object mapping the
// cookbook name to its API URL (e.g. {"apache2": ".../cookbooks/apache2"}),
// NOT as an array of cookbook records — and it omits a relationship key
// entirely when it's empty, so an absent group decodes to a nil map. The
// contract suite (contract_live_test.go) is what pins this shape against the
// live API.
type UserCookbooks struct {
	Owns         map[string]string `json:"owns"`
	Collaborates map[string]string `json:"collaborates"`
	Follows      map[string]string `json:"follows"`
}

// UserTools groups the tool relationships on a user profile. Like
// UserCookbooks, Supermarket maps each tool name to its API URL and omits
// the key when there are none.
type UserTools struct {
	Owns map[string]string `json:"owns"`
}

// UsersService accesses the /users endpoints.
type UsersService struct{ client *Client }

// Get returns a user profile by username.
func (s *UsersService) Get(ctx context.Context, username string) (*User, *Response, error) {
	if err := requireArg("username", username); err != nil {
		return nil, nil, err
	}
	u, resp, err := doJSON[User](ctx, s.client, request{
		method: http.MethodGet,
		path:   "/api/v1/users/" + url.PathEscape(username),
	})
	if err != nil {
		return nil, resp, err
	}
	return &u, resp, nil
}
