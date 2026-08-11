package usecase

import (
	"context"

	"github.com/lexa044/realtime-api/internal/domain"
)

type userService struct {
	users UserRepository
}

func NewUserService(users UserRepository) UserService {
	return &userService{users: users}
}

func (s *userService) GetProfile(ctx context.Context, userID string) (*domain.User, error) {
	return s.users.GetByID(ctx, userID)
}
