package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lexa044/realtime-api/internal/domain"
	"github.com/lexa044/realtime-api/internal/usecase"
)

func TestUserService_GetProfile_DelegatesToRepository(t *testing.T) {
	want := &domain.User{ID: "user-1", Username: "alice"}
	users := &fakeUserRepository{
		getByIDFn: func(ctx context.Context, id string) (*domain.User, error) {
			if id != "user-1" {
				t.Fatalf("expected id %q, got %q", "user-1", id)
			}
			return want, nil
		},
	}
	svc := usecase.NewUserService(users)

	got, err := svc.GetProfile(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatal("expected the same user returned by the repository")
	}
}

func TestUserService_GetProfile_NotFound(t *testing.T) {
	users := &fakeUserRepository{
		getByIDFn: func(ctx context.Context, id string) (*domain.User, error) { return nil, domain.ErrUserNotFound },
	}
	svc := usecase.NewUserService(users)

	_, err := svc.GetProfile(context.Background(), "missing")
	if !errors.Is(err, domain.ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}
