package types_test

import (
	"interchange-nel/x/dex/types"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

func OrderListToSellOrderBook(list []types.Order) types.SellOrderBook {
	listCopy := make([]*types.Order, len(list))

	for i, order := range list {
		order := order
		listCopy[i] = &order
	}

	book := types.SellOrderBook{
		AmountDenom: "foo",
		PriceDenom:  "bar",
		Book: &types.OrderBook{
			IdCount: 0,
			Orders:  listCopy,
		},
	}

	return book
}

type liquidateBuyRes struct {
	Book       []types.Order
	Remaining  types.Order
	Liquidated types.Order
	Purchase   int32
	Match      bool
	Filled     bool
}

type fillBuyRes struct {
	Book       []types.Order
	Remaining  types.Order
	Liquidated []types.Order
	Purchase   int32
	Filled     bool
}

func TestSellOrderBook_AppendOrder(t *testing.T) {
	sellBook := types.NewSellOrderBook(GenPair())

	// prevent zero amount
	seller, amount, price := GenOrder()
	_, err := sellBook.AppendOrder(seller, 0, price)
	require.ErrorIs(t, err, types.ErrZeroAmount)

	// prevent big amount
	_, err = sellBook.AppendOrder(seller, types.MaxAmount+1, price)
	require.ErrorIs(t, err, types.ErrMaxAmount)

	// prevent zero price
	_, err = sellBook.AppendOrder(seller, amount, 0)
	require.ErrorIs(t, err, types.ErrZeroPrice)

	// prevent big price
	_, err = sellBook.AppendOrder(seller, amount, types.MaxPrice+1)
	require.ErrorIs(t, err, types.ErrMaxPrice)

	// can append sell orders
	for i := 0; i < 20; i++ {
		// append a new order
		creator, amount, price := GenOrder()
		newOrder := types.Order{
			Id:      sellBook.Book.IdCount,
			Creator: creator,
			Amount:  amount,
			Price:   price,
		}
		orderID, err := sellBook.AppendOrder(creator, amount, price)

		// assert checks
		require.NoError(t, err)
		require.Contains(t, sellBook.Book.Orders, &newOrder)
		require.Equal(t, newOrder.Id, orderID)
	}

	// assert checks
	require.Len(t, sellBook.Book.Orders, 20)
	require.True(t, sort.SliceIsSorted(sellBook.Book.Orders, func(i, j int) bool {
		return sellBook.Book.Orders[i].Price > sellBook.Book.Orders[j].Price
	}))
}

func simulateLiquidateFromBuyOrder(
	t *testing.T,
	inputList []types.Order,
	inputOrder types.Order,
	expected liquidateBuyRes,
) {
	book := OrderListToSellOrderBook(inputList)
	expectedBook := OrderListToSellOrderBook(expected.Book)
	require.True(t, sort.SliceIsSorted(book.Book.Orders, func(i, j int) bool {
		return book.Book.Orders[i].Price > book.Book.Orders[j].Price
	}))
	require.True(t, sort.SliceIsSorted(expectedBook.Book.Orders, func(i, j int) bool {
		return expectedBook.Book.Orders[i].Price > expectedBook.Book.Orders[j].Price
	}))

	remaining, liquidated, purchase, match, filled := book.LiquidateFromBuyOrder(inputOrder)

	require.Equal(t, expectedBook, book)
	require.Equal(t, expected.Remaining, remaining)
	require.Equal(t, expected.Liquidated, liquidated)
	require.Equal(t, expected.Purchase, purchase)
	require.Equal(t, expected.Match, match)
	require.Equal(t, expected.Filled, filled)
}

func TestLiquidateFromBuyOrder(t *testing.T) {
	// no match for empty book
	inputOrder := types.Order{Id: 10, Creator: MockAccount("1"), Amount: 100, Price: 10}
	book := OrderListToSellOrderBook([]types.Order{})
	_, _, _, match, _ := book.LiquidateFromBuyOrder(inputOrder)
	require.False(t, match)

	// sell book
	inputBook := []types.Order{
		{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
		{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
		{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
	}

	// test no match if lowest ask too high (25 < 30)
	book = OrderListToSellOrderBook(inputBook)
	_, _, _, match, _ = book.LiquidateFromBuyOrder(inputOrder)
	require.False(t, match)

	// entirely filled (30 > 15)
	inputOrder = types.Order{Id: 10, Creator: MockAccount("1"), Amount: 20, Price: 30}
	expected := liquidateBuyRes{
		Book: []types.Order{
			{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
			{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
			{Id: 2, Creator: MockAccount("2"), Amount: 10, Price: 15},
		},
		Remaining:  types.Order{Id: 10, Creator: MockAccount("1"), Amount: 0, Price: 30},
		Liquidated: types.Order{Id: 2, Creator: MockAccount("2"), Amount: 20, Price: 15},
		Purchase:   int32(20),
		Match:      true,
		Filled:     true,
	}
	simulateLiquidateFromBuyOrder(t, inputBook, inputOrder, expected)

	// entirely filled (30 = 30)
	inputOrder = types.Order{Id: 10, Creator: MockAccount("1"), Amount: 30, Price: 30}
	expected = liquidateBuyRes{
		Book: []types.Order{
			{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
			{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
		},
		Remaining:  types.Order{Id: 10, Creator: MockAccount("1"), Amount: 0, Price: 30},
		Liquidated: types.Order{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
		Purchase:   int32(30),
		Match:      true,
		Filled:     true,
	}
	simulateLiquidateFromBuyOrder(t, inputBook, inputOrder, expected)

	// not filled and entirely liquidated (60 > 30)
	inputOrder = types.Order{Id: 10, Creator: MockAccount("1"), Amount: 60, Price: 30}
	expected = liquidateBuyRes{
		Book: []types.Order{
			{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
			{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
		},
		Remaining:  types.Order{Id: 10, Creator: MockAccount("1"), Amount: 30, Price: 30},
		Liquidated: types.Order{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
		Purchase:   int32(30),
		Match:      true,
		Filled:     false,
	}
	simulateLiquidateFromBuyOrder(t, inputBook, inputOrder, expected)
}

func simulateFillBuyOrder(
	t *testing.T,
	inputList []types.Order,
	inputOrder types.Order,
	expected fillBuyRes,
) {
	book := OrderListToSellOrderBook(inputList)
	expectedBook := OrderListToSellOrderBook(expected.Book)

	require.True(t, sort.SliceIsSorted(book.Book.Orders, func(i, j int) bool {
		return book.Book.Orders[i].Price > book.Book.Orders[j].Price
	}))
	require.True(t, sort.SliceIsSorted(expectedBook.Book.Orders, func(i, j int) bool {
		return expectedBook.Book.Orders[i].Price > expectedBook.Book.Orders[j].Price
	}))

	remaining, liquidated, purchase, filled := book.FillBuyOrder(inputOrder)

	require.Equal(t, expectedBook, book)
	require.Equal(t, expected.Remaining, remaining)
	require.Equal(t, expected.Liquidated, liquidated)
	require.Equal(t, expected.Purchase, purchase)
	require.Equal(t, expected.Filled, filled)
}

func TestFillBuyOrder(t *testing.T) {
	var inputBook []types.Order

	// empty book
	inputOrder := types.Order{Id: 10, Creator: MockAccount("1"), Amount: 30, Price: 10}
	expected := fillBuyRes{
		Book:       []types.Order{},
		Remaining:  inputOrder,
		Liquidated: []types.Order(nil),
		Purchase:   int32(0),
		Filled:     false,
	}
	simulateFillBuyOrder(t, inputBook, inputOrder, expected)

	// no match
	inputBook = []types.Order{
		{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
		{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
		{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
	}
	expected = fillBuyRes{
		Book:       inputBook,
		Remaining:  inputOrder,
		Liquidated: []types.Order(nil),
		Purchase:   int32(0),
		Filled:     false,
	}
	simulateFillBuyOrder(t, inputBook, inputOrder, expected)

	// first order liquidated, not filled
	inputOrder = types.Order{Id: 10, Creator: MockAccount("1"), Amount: 60, Price: 18}
	expected = fillBuyRes{
		Book: []types.Order{
			{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
			{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
		},
		Remaining: types.Order{Id: 10, Creator: MockAccount("1"), Amount: 30, Price: 18},
		Liquidated: []types.Order{
			{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
		},
		Purchase: int32(30),
		Filled:   false,
	}
	simulateFillBuyOrder(t, inputBook, inputOrder, expected)

	// filled with two order
	inputOrder = types.Order{Id: 10, Creator: MockAccount("1"), Amount: 60, Price: 22}
	expected = fillBuyRes{
		Book: []types.Order{
			{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
			{Id: 1, Creator: MockAccount("1"), Amount: 170, Price: 20},
		},
		Remaining: types.Order{Id: 10, Creator: MockAccount("1"), Amount: 0, Price: 22},
		Liquidated: []types.Order{
			{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
			{Id: 1, Creator: MockAccount("1"), Amount: 30, Price: 20},
		},
		Purchase: int32(30 + 30),
		Filled:   true,
	}
	simulateFillBuyOrder(t, inputBook, inputOrder, expected)

	// not filled, sell order book liquidated
	inputOrder = types.Order{Id: 10, Creator: MockAccount("1"), Amount: 300, Price: 30}
	expected = fillBuyRes{
		Book:      []types.Order{},
		Remaining: types.Order{Id: 10, Creator: MockAccount("1"), Amount: 20, Price: 30},
		Liquidated: []types.Order{
			{Id: 2, Creator: MockAccount("2"), Amount: 30, Price: 15},
			{Id: 1, Creator: MockAccount("1"), Amount: 200, Price: 20},
			{Id: 0, Creator: MockAccount("0"), Amount: 50, Price: 25},
		},
		Purchase: int32(30 + 200 + 50),
		Filled:   false,
	}
	simulateFillBuyOrder(t, inputBook, inputOrder, expected)
}
