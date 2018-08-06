package main

type InMemoryUserStore struct {
	UsersByAccessToken map[string]*User
}

func NewInMemoryUserStore() (*InMemoryUserStore) {
	return &InMemoryUserStore{
		UsersByAccessToken: make(map[string]*User),
	}
}

func (store InMemoryUserStore) setUser(accessToken string, user *User) (error) {
	store.UsersByAccessToken[accessToken] = user
	return nil
}

func (store InMemoryUserStore) getUser(accessToken string) (*User, error) {
	return store.UsersByAccessToken[accessToken], nil
}