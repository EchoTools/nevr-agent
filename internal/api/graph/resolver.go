package graph

import (
	"go.mongodb.org/mongo-driver/mongo"
)

// Resolver is the root resolver for the GraphQL schema
type Resolver struct {
	MongoClient *mongo.Client
}

// NewResolver creates a new resolver with the given MongoDB client
func NewResolver(mongoClient *mongo.Client) *Resolver {
	return &Resolver{
		MongoClient: mongoClient,
	}
}
