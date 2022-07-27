package types

func NewBuyOrderBook(amountDenom string, priceDenom string) BuyOrderBook {
	book := NewOrderBook()
	return BuyOrderBook{
		AmountDenom: amountDenom,
		PriceDenom:  priceDenom,
		Book:        &book,
	}
}
