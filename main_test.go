package main

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_parseAuthorizedKeys(t *testing.T) {
	tests := []struct {
		name string
		s    string
		out  map[string]struct{}
		ok   bool
	}{
		{
			name: "empty",
			ok:   true,
			out:  map[string]struct{}{},
		},
		{
			name: "malformed",
			s: `
# a comment

bad
			`,
		},
		{
			name: "ok",
			s: `
# a comment

ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDDkvg9+NTySctVaMkbZGwTRIUiQSo4crGWQPeFTi/XM3KhcUY+WduwHChJX1h03/DKJps8wtHUn3LmUKFR4BoJEgt8Od+L6ey5sev4lvPa2wDc5HJfervgCnVt9aomdFqeZUe6g4BDdPLUGbzT3T+A+08ocXy/eVv9Kke7Ka6GslJQQ5TBjW0AbPhxu6QmoZDb0tiWf9CwyVpiox5+vW7E+O6U1QOKT45Ellc2smHSAcI1gUDborS0GhFSso9SagMxcWNbZf8920DeaLs5tb8uwKfWKqHJfkY+VK3QuufpWZM3BJTPa0PePd75NRra2BOV4LDwGlLrZjOCULlYawDlDOIm6rpC3QV7juHTFWjS8ImvbsyEWZSE9N6klDMc23Zl9vhqJcG4U9LVAv2QMcr8aXBnmSo49rkd7/H6yHZgWqmrAijloZkiwsTbofT+lQx3JLEagk1rd8rmCp4F7WeUShvvmTq0tyPDutIhd1TXwLB0gyFObCDgb3CrXPtsACc= test RSA
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ6PAHCvJTosPqBppE6lmjjRt9Qlcisqx+DXt7jIbLba test ed25519
			`,
			out: map[string]struct{}{
				"SHA256:QV5G0CjjoGyh8J6oAbOxxV7hrQxEPSMxCxSXueLHfqU": {},
				"SHA256:4eVkakUHGhUT7z7rgKtv2Enlgvz52UnjT3qFWMA3gTU": {},
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := parseAuthorizedKeys(strings.NewReader(strings.TrimSpace(tt.s)))
			if tt.ok && err != nil {
				t.Fatalf("failed to parse authorized keys: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if err != nil {
				t.Logf("err: %v", err)
				return
			}

			if diff := cmp.Diff(tt.out, out); diff != "" {
				t.Fatalf("unexpected authorized keys map (-want +got):\n%s", diff)
			}
		})
	}
}
