package service

import (
	"context"
	"fmt"

	"github.com/epg-sync/epgsync/internal/model"
	"github.com/epg-sync/epgsync/internal/repository"
	"github.com/epg-sync/epgsync/pkg/errors"
	"github.com/epg-sync/epgsync/pkg/utils"
)

type UserService struct {
	userRepo       repository.UserRepository
	jwtSecret      string
	jwtExpireHours int
}

func NewUserService(userRepo repository.UserRepository, jwtSecret string, jwtExpireHours int) *UserService {
	return &UserService{
		userRepo:       userRepo,
		jwtSecret:      jwtSecret,
		jwtExpireHours: jwtExpireHours,
	}
}

// Register 注册新用户
func (s *UserService) Register(ctx context.Context, username, password, email string) (*model.User, error) {
	// 检查用户名是否已存在
	existing, _ := s.userRepo.GetByUsername(ctx, username)
	if existing != nil {
		return nil, errors.New(errors.ErrCodeAlreadyExists, "username already exists")
	}

	// 加密密码
	hashedPassword, err := utils.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &model.User{
		Username: username,
		Password: hashedPassword,
		Email:    email,
		Role:     "admin",
		IsActive: 1,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// Login 用户登录
func (s *UserService) Login(ctx context.Context, username, password string) (string, *model.User, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return "", nil, errors.New(errors.ErrCodeUnauthorized, "invalid username or password")
	}

	if user.IsActive != 1 {
		return "", nil, errors.New(errors.ErrCodeForbidden, "user account is disabled")
	}

	if !utils.CheckPassword(user.Password, password) {
		return "", nil, errors.New(errors.ErrCodeUnauthorized, "invalid username or password")
	}

	// 生成token (24小时有效)
	token, err := utils.GenerateToken(user.ID, user.Username, user.Role, s.jwtSecret, s.jwtExpireHours)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return token, user, nil
}

// GetUserByID 根据ID获取用户
func (s *UserService) GetUserByID(ctx context.Context, id int64) (*model.User, error) {
	return s.userRepo.GetByID(ctx, id)
}

// ListUsers 获取用户列表
func (s *UserService) ListUsers(ctx context.Context, page, pageSize int) ([]*model.User, int64, error) {
	offset := (page - 1) * pageSize
	return s.userRepo.List(ctx, offset, pageSize)
}

// UpdateUser 更新用户信息
func (s *UserService) UpdateUser(ctx context.Context, user *model.User) error {
	return s.userRepo.Update(ctx, user)
}

// DeleteUser 删除用户
func (s *UserService) DeleteUser(ctx context.Context, id int64) error {
	return s.userRepo.Delete(ctx, id)
}

// ChangePassword 修改密码
func (s *UserService) ChangePassword(ctx context.Context, userID int64, oldPassword, newPassword string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	if !utils.CheckPassword(user.Password, oldPassword) {
		return errors.New(errors.ErrCodeUnauthorized, "invalid old password")
	}

	hashedPassword, err := utils.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.Password = hashedPassword
	return s.userRepo.Update(ctx, user)
}
