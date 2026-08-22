package user_tools

import (
	"context"
	"fmt"

	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/domain"
	"github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/core"
	helper "github.com/leventsg/e-commerce-AI-system/services/aiagent/internal/tools/helper"
	"github.com/leventsg/e-commerce-AI-system/services/users/usersclient"
	"google.golang.org/grpc"
)

type UserQueryRPC interface {
	GetUser(ctx context.Context, in *usersclient.GetUserRequest, opts ...grpc.CallOption) (*usersclient.GetUserResponse, error)
	ListAddresses(ctx context.Context, in *usersclient.AllAddressLitstRequest, opts ...grpc.CallOption) (*usersclient.AddressListResponse, error)
	GetAddress(ctx context.Context, in *usersclient.GetAddressRequest, opts ...grpc.CallOption) (*usersclient.GetAddressResponse, error)
}

type UserWriteRPC interface {
	UpdateUser(ctx context.Context, in *usersclient.UpdateUserRequest, opts ...grpc.CallOption) (*usersclient.UpdateUserResponse, error)
	AddAddress(ctx context.Context, in *usersclient.AddAddressRequest, opts ...grpc.CallOption) (*usersclient.AddAddressResponse, error)
	UpdateAddress(ctx context.Context, in *usersclient.UpdateAddressRequest, opts ...grpc.CallOption) (*usersclient.UpdateAddressResponse, error)
	DeleteAddress(ctx context.Context, in *usersclient.DeleteAddressRequest, opts ...grpc.CallOption) (*usersclient.DeleteAddressResponse, error)
}

func UserQueryHandlers(rpc UserQueryRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolUserGet:     userGetHandler(rpc),
		domain.ToolAddressList: addressListHandler(rpc),
		domain.ToolAddressGet:  addressGetHandler(rpc),
	}
}

func UserWriteHandlers(rpc UserWriteRPC) map[string]core.HandlerFunc {
	if rpc == nil {
		return nil
	}
	return map[string]core.HandlerFunc{
		domain.ToolUserUpdate:    userUpdateHandler(rpc),
		domain.ToolAddressAdd:    addressAddHandler(rpc),
		domain.ToolAddressUpdate: addressUpdateHandler(rpc),
		domain.ToolAddressDelete: addressDeleteHandler(rpc),
	}
}

func userGetHandler(rpc UserQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := authenticatedUserID(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.GetUser(ctx, &usersclient.GetUserRequest{UserId: userID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("user_get rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: user_get returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("user_get", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data: map[string]any{
				"user": map[string]any{
					"user_id":    resp.UserId,
					"email":      resp.Email,
					"user_name":  resp.UserName,
					"avatar_url": resp.AvatarUrl,
					"created_at": resp.CreatedAt,
					"updated_at": resp.UpdatedAt,
					"logout_at":  resp.LogoutAt,
				},
			},
			Summary: fmt.Sprintf("已查询用户“%s”的资料。", resp.UserName),
		}, nil
	}
}

func userUpdateHandler(rpc UserWriteRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := authenticatedUserID(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		userName, err := helper.OptionalStringArgument(req.Arguments, "user_name")
		if err != nil {
			return core.HandlerResult{}, err
		}
		avatarURL, err := helper.OptionalStringArgument(req.Arguments, "avatar_url")
		if err != nil {
			return core.HandlerResult{}, err
		}
		if userName == "" && avatarURL == "" {
			return core.HandlerResult{}, helper.InvalidArgument("user_name", "or avatar_url is required")
		}
		resp, err := rpc.UpdateUser(ctx, &usersclient.UpdateUserRequest{
			UserId:    userID,
			UsrName:   userName,
			AvatarUrl: avatarURL,
			Ip:        clientIPFromContext(ctx),
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("user_update rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("user_update returned nil response")
		}
		if err := helper.ValidateRPCResponse("user_update", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data: map[string]any{
				"user_id":    resp.UserId,
				"user_name":  resp.UserName,
				"avatar_url": resp.AvatarUrl,
			},
			Summary: "用户资料已更新。",
		}, nil
	}
}

func addressListHandler(rpc UserQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := authenticatedUserID(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.ListAddresses(ctx, &usersclient.AllAddressLitstRequest{UserId: userID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("address_list rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: address_list returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("address_list", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		addresses := compactAddresses(resp.Data)
		return core.HandlerResult{
			Data:    map[string]any{"total": len(addresses), "addresses": addresses},
			Summary: fmt.Sprintf("查询到 %d 个收货地址。", len(addresses)),
		}, nil
	}
}

func addressGetHandler(rpc UserQueryRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, addressID, err := addressTarget(req)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.GetAddress(ctx, &usersclient.GetAddressRequest{UserId: userID, AddressId: addressID})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("address_get rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: address_get returned nil response", helper.ErrQueryRPCUnavailable)
		}
		if err := helper.ValidateRPCResponse("address_get", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		if resp.Data == nil {
			return core.HandlerResult{}, fmt.Errorf("%w: address_get returned empty address", helper.ErrQueryRPCUnavailable)
		}
		return core.HandlerResult{
			Data:    map[string]any{"address": compactAddress(resp.Data)},
			Summary: fmt.Sprintf("已查询地址 %d。", addressID),
		}, nil
	}
}

func addressAddHandler(rpc UserWriteRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, err := authenticatedUserID(req.UserID)
		if err != nil {
			return core.HandlerResult{}, err
		}
		address, err := addressArguments(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.AddAddress(ctx, &usersclient.AddAddressRequest{
			UserId:          userID,
			RecipientName:   address.recipientName,
			PhoneNumber:     address.phoneNumber,
			Province:        address.province,
			City:            address.city,
			DetailedAddress: address.detailedAddress,
			IsDefault:       address.isDefault,
			Ip:              clientIPFromContext(ctx),
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("address_add rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("address_add returned nil response")
		}
		if err := helper.ValidateRPCResponse("address_add", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data:    map[string]any{"address": compactAddress(resp.Data)},
			Summary: "收货地址已添加。",
		}, nil
	}
}

func addressUpdateHandler(rpc UserWriteRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, addressID, err := addressTarget(req)
		if err != nil {
			return core.HandlerResult{}, err
		}
		address, err := addressArguments(req.Arguments)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.UpdateAddress(ctx, &usersclient.UpdateAddressRequest{
			UserId:          userID,
			AddressId:       addressID,
			RecipientName:   address.recipientName,
			PhoneNumber:     address.phoneNumber,
			Province:        address.province,
			City:            address.city,
			DetailedAddress: address.detailedAddress,
			IsDefault:       address.isDefault,
			Ip:              clientIPFromContext(ctx),
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("address_update rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("address_update returned nil response")
		}
		if err := helper.ValidateRPCResponse("address_update", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data:    map[string]any{"address": compactAddress(resp.Data)},
			Summary: fmt.Sprintf("地址 %d 已更新。", addressID),
		}, nil
	}
}

func addressDeleteHandler(rpc UserWriteRPC) core.HandlerFunc {
	return func(ctx context.Context, req core.HandlerRequest) (core.HandlerResult, error) {
		userID, addressID, err := addressTarget(req)
		if err != nil {
			return core.HandlerResult{}, err
		}
		resp, err := rpc.DeleteAddress(ctx, &usersclient.DeleteAddressRequest{
			UserId:    userID,
			AddressId: addressID,
			Ip:        clientIPFromContext(ctx),
		})
		if err != nil {
			return core.HandlerResult{}, fmt.Errorf("address_delete rpc: %w", err)
		}
		if resp == nil {
			return core.HandlerResult{}, fmt.Errorf("address_delete returned nil response")
		}
		if err := helper.ValidateRPCResponse("address_delete", resp, int64(resp.StatusCode), resp.StatusMsg); err != nil {
			return core.HandlerResult{}, err
		}
		return core.HandlerResult{
			Data:    map[string]any{"address_id": addressID},
			Summary: fmt.Sprintf("地址 %d 已删除。", addressID),
		}, nil
	}
}

type addressArgs struct {
	recipientName   string
	phoneNumber     string
	province        string
	city            string
	detailedAddress string
	isDefault       bool
}

func authenticatedUserID(userID uint64) (uint32, error) {
	value, err := helper.AuthenticatedUserID32(userID)
	if err != nil {
		return 0, err
	}
	return uint32(value), nil
}

func addressTarget(req core.HandlerRequest) (uint32, int32, error) {
	userID, err := authenticatedUserID(req.UserID)
	if err != nil {
		return 0, 0, err
	}
	addressValue, err := helper.RequiredInt64Argument(req.Arguments, "address_id")
	if err != nil {
		return 0, 0, err
	}
	addressID, err := helper.PositiveInt32(addressValue, "address_id")
	if err != nil {
		return 0, 0, err
	}
	return userID, addressID, nil
}

func addressArguments(args map[string]any) (addressArgs, error) {
	recipientName, err := helper.RequiredStringArgument(args, "recipient_name")
	if err != nil {
		return addressArgs{}, err
	}
	phoneNumber, err := helper.RequiredStringArgument(args, "phone_number")
	if err != nil {
		return addressArgs{}, err
	}
	province, err := helper.RequiredStringArgument(args, "province")
	if err != nil {
		return addressArgs{}, err
	}
	city, err := helper.RequiredStringArgument(args, "city")
	if err != nil {
		return addressArgs{}, err
	}
	detailedAddress, err := helper.RequiredStringArgument(args, "detailed_address")
	if err != nil {
		return addressArgs{}, err
	}
	isDefault, err := optionalBoolArgument(args, "is_default")
	if err != nil {
		return addressArgs{}, err
	}
	return addressArgs{
		recipientName:   recipientName,
		phoneNumber:     phoneNumber,
		province:        province,
		city:            city,
		detailedAddress: detailedAddress,
		isDefault:       isDefault,
	}, nil
}

func optionalBoolArgument(args map[string]any, name string) (bool, error) {
	value, ok := args[name]
	if !ok || value == nil {
		return false, nil
	}
	typed, ok := value.(bool)
	if !ok {
		return false, helper.InvalidArgument(name, "must be a boolean")
	}
	return typed, nil
}

func clientIPFromContext(ctx context.Context) string {
	if execution, ok := helper.ToolExecutionFromContext(ctx); ok {
		return execution.ClientIP
	}
	return ""
}

func compactAddresses(values []*usersclient.AddressData) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if value != nil {
			result = append(result, compactAddress(value))
		}
	}
	return result
}

func compactAddress(value *usersclient.AddressData) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return map[string]any{
		"address_id":       value.AddressId,
		"recipient_name":   value.RecipientName,
		"phone_number":     value.PhoneNumber,
		"province":         value.Province,
		"city":             value.City,
		"detailed_address": value.DetailedAddress,
		"is_default":       value.IsDefault,
		"created_at":       value.CreatedAt,
		"updated_at":       value.UpdatedAt,
	}
}
