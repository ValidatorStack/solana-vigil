package rpc

import (
	"context"
	"fmt"
)

// ValidateSameCluster prevents a reference endpoint on another cluster from
// producing dangerously plausible slot-lag and baseline metrics.
func ValidateSameCluster(ctx context.Context, local, reference *Client) (string, error) {
	localHash, err := local.GetGenesisHash(ctx)
	if err != nil {
		return "", fmt.Errorf("local genesis hash: %w", err)
	}
	if reference == nil {
		return localHash, nil
	}
	referenceHash, err := reference.GetGenesisHash(ctx)
	if err != nil {
		return "", fmt.Errorf("reference genesis hash: %w", err)
	}
	if localHash != referenceHash {
		return "", fmt.Errorf("local and reference RPCs belong to different clusters")
	}
	return localHash, nil
}
