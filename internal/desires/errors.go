package desires

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func IsNotFoundError(err error) bool {
	return status.Code(err) == codes.NotFound
}

func IsAlreadyExistsError(err error) bool {
	return status.Code(err) == codes.AlreadyExists
}

func IsPreconditionFailedError(err error) bool {
	return status.Code(err) == codes.FailedPrecondition
}

func NewNotFoundError() error {
	return status.Error(codes.NotFound, "not found")
}

func NewPreconditionFailedError() error {
	return status.Error(codes.FailedPrecondition, "precondition failed")
}
