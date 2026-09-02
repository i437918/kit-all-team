package gitx

import (
	"context"
	"fmt"
	"strings"
)

var reservedContentRoots = []string{".env", ".teamkit", "db"}

func (r Repository) validateFetchedContentTree(ctx context.Context, directory, commitRef string, auth Credentials) error {
	if err := validateGitMutationMetadata(directory); err != nil {
		return err
	}
	result, err := r.runResult(ctx, Command{
		Args: hardenedArgs(directory, "ls-tree", "-z", "--name-only", commitRef),
		Env:  localEnvironment(),
	}, auth)
	if err != nil {
		return err
	}
	for _, path := range strings.Split(result.Stdout, "\x00") {
		root := path
		if separator := strings.IndexAny(root, `/\`); separator >= 0 {
			root = root[:separator]
		}
		root = strings.TrimRight(root, " .")
		for _, reserved := range reservedContentRoots {
			if strings.EqualFold(root, reserved) {
				return &Error{
					Code: "GIT_RESERVED_PATH_COLLISION",
					Err:  fmt.Errorf("fetched content tracks a Team Kit-reserved root path"),
				}
			}
		}
	}
	return nil
}
