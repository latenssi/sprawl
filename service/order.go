package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"strings"

	"github.com/sprawl/sprawl/errors"
	"github.com/sprawl/sprawl/interfaces"
	"github.com/sprawl/sprawl/pb"
	"github.com/golang/protobuf/proto"
	ptypes "github.com/golang/protobuf/ptypes"
)

// OrderService implements the OrderService Server service.proto
type OrderService struct {
	Logger  interfaces.Logger
	Storage interfaces.Storage
	P2p     interfaces.P2p
}

func getOrderStorageKey(orderID []byte) []byte {
	return []byte(strings.Join([]string{string(interfaces.OrderPrefix), string(orderID)}, ""))
}

// RegisterStorage registers a storage service to store the Orders in
func (s *OrderService) RegisterStorage(storage interfaces.Storage) {
	s.Storage = storage
}

// RegisterP2p registers a p2p service
func (s *OrderService) RegisterP2p(p2p interfaces.P2p) {
	s.P2p = p2p
}

// Create creates an Order, storing it locally and broadcasts the Order to all other nodes on the channel
func (s *OrderService) Create(ctx context.Context, in *pb.CreateRequest) (*pb.CreateResponse, error) {
	// Get current timestamp as protobuf type
	now := ptypes.TimestampNow()

	// TODO: Use the node's private key here as a secret to sign the Order ID with
	secret := "mysecret"

	// Create a new HMAC by defining the hash type and the key (as byte array)
	h := hmac.New(sha256.New, []byte(secret))

	// Write Data to it
	h.Write(append([]byte(in.String()), []byte(now.String())...))

	// Get result and encode as hexadecimal string
	id := h.Sum(nil)

	// Construct the order
	order := &pb.Order{
		Id:           id,
		Created:      now,
		Asset:        in.Asset,
		CounterAsset: in.CounterAsset,
		Amount:       in.Amount,
		Price:        in.Price,
		State:        pb.State_OPEN,
	}

	// Get order as bytes
	orderInBytes, err := proto.Marshal(order)
	if !errors.IsEmpty(err) {
		if s.Logger != nil {
			s.Logger.Warn(errors.E(errors.Op("Marshal order"), err))
		}
	}
	// Save order to LevelDB locally
	err = s.Storage.Put(getOrderStorageKey(id), orderInBytes)
	if !errors.IsEmpty(err) {
		err = errors.E(errors.Op("Put order"), err)
		
	}
	// Construct the message to send to other peers
	wireMessage := &pb.WireMessage{ChannelID: in.GetChannelID(), Operation: pb.Operation_CREATE, Data: orderInBytes}

	if s.P2p != nil {
		// Send the order creation by wire
		s.P2p.Send(wireMessage)
	} else {
		if s.Logger != nil {
			s.Logger.Warn("P2p service not registered with OrderService, not publishing or receiving orders from the network!")
		}
	}

	return &pb.CreateResponse{
		CreatedOrder: order,
		Error:        nil,
	}, err
}

// Receive receives a buffer from p2p and tries to unmarshal it into a struct
func (s *OrderService) Receive(buf []byte) error {
	wireMessage := &pb.WireMessage{}
	err := proto.Unmarshal(buf, wireMessage)
	if !errors.IsEmpty(err) {
		if s.Logger != nil {
			s.Logger.Warn(errors.E(errors.Op("Unmarshal wiremessage proto in Receive"), err))
		}
		return errors.E(errors.Op("Unmarshal wiremessage proto in Receive"), err)
	}

	op := wireMessage.GetOperation()
	data := wireMessage.GetData()
	order := &pb.Order{}
	err = proto.Unmarshal(data, order)
	if !errors.IsEmpty(err) {
		if s.Logger != nil {
			s.Logger.Warn(errors.E(errors.Op("Unmarshal order proto in Receive"), err))
		}
		return errors.E(errors.Op("Unmarshal order proto in Receive"), err)
	}

	if s.Storage != nil {
		switch op {
		case pb.Operation_CREATE:
			// Save order to LevelDB locally
			err = s.Storage.Put(getOrderStorageKey(order.GetId()), data)
			if !errors.IsEmpty(err) {
				err = errors.E(errors.Op("Put order"), err)
			}
		case pb.Operation_DELETE:
			err = s.Storage.Delete(getOrderStorageKey(order.GetId()))
			if !errors.IsEmpty(err) {
				err = errors.E(errors.Op("Put order"), err)
			}
		}
	} else {
		if s.Logger != nil {
			s.Logger.Warn("Storage not registered with OrderService, not persisting Orders!")
		}
	}

	return err
}

// GetOrder fetches a single order from the database
func (s *OrderService) GetOrder(ctx context.Context, in *pb.OrderSpecificRequest) (*pb.Order, error) {
	data, err := s.Storage.Get(getOrderStorageKey(in.GetOrderID()))
	if !errors.IsEmpty(err) {
		return nil, errors.E(errors.Op("Get order"), err)
	}
	order := &pb.Order{}
	proto.Unmarshal(data, order)
	return order, nil
}

// GetAllOrders fetches all orders from the database
func (s *OrderService) GetAllOrders(ctx context.Context, in *pb.Empty) (*pb.OrderListResponse, error) {
	data, err := s.Storage.GetAllWithPrefix(string(interfaces.OrderPrefix))
	if !errors.IsEmpty(err) {
		return nil, errors.E(errors.Op("Get all orders"), err)
	}

	orders := make([]*pb.Order, 0)
	i := 0
	for _, value := range data {
		order := &pb.Order{}
		proto.Unmarshal([]byte(value), order)
		orders = append(orders, order)
		i++
	}

	orderListResponse := &pb.OrderListResponse{Orders: orders}
	return orderListResponse, nil
}

// Delete removes the Order with the specified ID locally, and broadcasts the same request to all other nodes on the channel
func (s *OrderService) Delete(ctx context.Context, in *pb.OrderSpecificRequest) (*pb.GenericResponse, error) {
	orderInBytes, err := s.Storage.Get(getOrderStorageKey(in.GetOrderID()))
	if !errors.IsEmpty(err) {
		return nil, errors.E(errors.Op("Delete order"), err)
	}

	// Construct the message to send to other peers
	wireMessage := &pb.WireMessage{ChannelID: in.GetChannelID(), Operation: pb.Operation_DELETE, Data: orderInBytes}

	if s.P2p != nil {
		// Send the order creation by wire
		s.P2p.Send(wireMessage)
	} else {
		if s.Logger != nil {
			s.Logger.Warn("P2p service not registered with OrderService, not publishing or receiving orders from the network!")
		}
	}

	// Try to delete the Order from LevelDB with specified ID
	err = s.Storage.Delete(getOrderStorageKey(in.GetOrderID()))
	if !errors.IsEmpty(err){
		err = errors.E(errors.Op("Delete order"), err)
	}

	return &pb.GenericResponse{
		Error: nil,
	}, err
}

// Lock locks the given Order if the Order is created by this node, broadcasts the lock to other nodes on the channel.
func (s *OrderService) Lock(ctx context.Context, in *pb.OrderSpecificRequest) (*pb.GenericResponse, error) {

	// TODO: Add Order locking logic

	return &pb.GenericResponse{
		Error: nil,
	}, nil
}

// Unlock unlocks the given Order if it's created by this node, broadcasts the unlocking operation to other nodes on the channel.
func (s *OrderService) Unlock(ctx context.Context, in *pb.OrderSpecificRequest) (*pb.GenericResponse, error) {

	// TODO: Add Order unlocking logic

	return &pb.GenericResponse{
		Error: nil,
	}, nil
}
