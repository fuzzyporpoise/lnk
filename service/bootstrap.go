package service

import (
	"context"
	"io"

	bootpkg "go.fuzzyporpoise.dev/lnk/v2/internal/bootstrapper"
)

// Bootstrap runs bootstrap.sh explicitly.
func (s *Service) Bootstrap(ctx context.Context, stdout, stderr io.Writer, stdin io.Reader) (bool, error) {
	if err := s.requireGitRepo(); err != nil {
		return false, err
	}
	runner := bootpkg.New(s.repoPath, s.git)
	script, err := runner.FindScript()
	if err != nil {
		return false, err
	}
	if script == "" {
		return false, nil
	}
	if err := runner.RunScript(ctx, script, stdout, stderr, stdin); err != nil {
		return true, err
	}
	return true, nil
}
