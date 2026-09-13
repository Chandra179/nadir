package qdrantutil

import (
	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
)

// Clients is the shared Qdrant client set used by the document store, history,
// and semantic cache adapters. Construct it once per process so those adapters
// share the same gRPC connection and client lifecycle.
type Clients struct {
	Points      qdrant.PointsClient
	Collections qdrant.CollectionsClient
}

// NewClients creates the Qdrant client set over an existing gRPC connection.
func NewClients(conn *grpc.ClientConn) Clients {
	return Clients{
		Points:      qdrant.NewPointsClient(conn),
		Collections: qdrant.NewCollectionsClient(conn),
	}
}
