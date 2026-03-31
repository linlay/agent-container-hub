package runtime

import (
	"errors"
	"testing"
)

func TestParseImageListOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		wantID  string
		wantRef string
		wantErr error
	}{
		{
			name:    "normal single line",
			raw:     "sha256:abc123\tdaily-office:latest\n",
			wantID:  "sha256:abc123",
			wantRef: "daily-office:latest",
		},
		{
			name:    "multiple lines returns first",
			raw:     "sha256:abc123\tdaily-office:latest\nsha256:def456\tdaily-office:v2\n",
			wantID:  "sha256:abc123",
			wantRef: "daily-office:latest",
		},
		{
			name:    "empty output",
			raw:     "",
			wantErr: ErrImageNotFound,
		},
		{
			name:    "whitespace only",
			raw:     "   \n  \n",
			wantErr: ErrImageNotFound,
		},
		{
			name:    "dangling image with none tag",
			raw:     "sha256:abc123\t<none>:<none>\n",
			wantID:  "sha256:abc123",
			wantRef: "<none>:<none>",
		},
		{
			name:    "id only without tab",
			raw:     "sha256:abc123\n",
			wantID:  "sha256:abc123",
			wantRef: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			info, err := parseImageListOutput(tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseImageListOutput() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseImageListOutput() error = %v, want nil", err)
			}
			if info.ID != tc.wantID {
				t.Fatalf("parseImageListOutput() ID = %q, want %q", info.ID, tc.wantID)
			}
			if info.Ref != tc.wantRef {
				t.Fatalf("parseImageListOutput() Ref = %q, want %q", info.Ref, tc.wantRef)
			}
		})
	}
}
