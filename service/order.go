package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"strings"

	"github.com/eqlabs/sprawl/interfaces"
	"github.com/eqlabs/sprawl/pb"
	"github.com/golang/protobuf/proto"
	ptypes "github.com/golang/protobuf/ptypes"
)

// OrderService implements the OrderService Server service.proto
type OrderService struct {
	storage interfaces.Storage
	p2p     interfaces.P2p
}

func getOrderStorageKey(orderID []byte) []byte {
	return []byte(strings.Join([]string{string(interfaces.OrderPrefix), string(orderID)}, ""))
}

// RegisterStorage registers a storage service to store the Orders in
func (s *OrderService) RegisterStorage(storage interfaces.Storage) {
	s.storage = storage
}

// RegisterP2p registers a p2p service
func (s *OrderService) RegisterP2p(p2p interfaces.P2p) {
	s.p2p = p2p
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

	// Save order to LevelDB locally
	err = s.storage.Put(getOrderStorageKey(id), orderInBytes)

	// Construct the message to send to other peers
	wireMessage := &pb.WireMessage{ChannelID: in.GetChannelID(), Operation: pb.Operation_CREATE, Data: orderInBytes}

	// Send the order creation by wire
	s.p2p.Send(wireMessage)

	return &pb.CreateResponse{
		CreatedOrder: order,
		Error:        nil,
	}, err
}

// Receive receives a buffer from p2p and tries to unmarshal it into a struct
func (s *OrderService) Receive(buf []byte) error {
	wireMessage := &pb.WireMessage{}

	err := proto.Unmarshal(buf, wireMessage)
	if err != nil {
		return err
	}

	op := wireMessage.GetOperation()
	data := wireMessage.GetData()
	order := &pb.Order{}
	err = proto.Unmarshal(data, order)
	if err != nil {
		return err
	}

	switch op {
	case pb.Operation_CREATE:
		// Save order to LevelDB locally
		err = s.storage.Put(getOrderStorageKey(order.GetId()), data)
	case pb.Operation_DELETE:
		err = s.storage.Delete(getOrderStorageKey(order.GetId()))
	}

	return err
}

// GetOrder fetches a single order from the database
func (s *OrderService) GetOrder(ctx context.Context, in *pb.OrderSpecificRequest) (*pb.Order, error) {
	data, err := s.storage.Get(getOrderStorageKey(in.GetOrderID()))
	if err != nil {
		return nil, err
	}
	order := &pb.Order{}
	proto.Unmarshal(data, order)
	return order, nil
}

// GetAllOrders fetches all orders from the database
func (s *OrderService) GetAllOrders(ctx context.Context, in *pb.Empty) (*pb.OrderListResponse, error) {
	data, err := s.storage.GetAllWithPrefix(string(interfaces.OrderPrefix))
	if err != nil {
		return nil, err
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
	orderInBytes, err := s.storage.Get(getOrderStorageKey(in.GetOrderID()))
	if err != nil {
		return nil, err
	}

	// Construct the message to send to other peers
	wireMessage := &pb.WireMessage{ChannelID: in.GetChannelID(), Operation: pb.Operation_DELETE, Data: orderInBytes}

	// Send the order creation by wire
	s.p2p.Send(wireMessage)

	// Try to delete the Order from LevelDB with specified ID
	err = s.storage.Delete(getOrderStorageKey(in.GetOrderID()))

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