package types

func NewSellOrderBook(amountDenom string, priceDenom string) SellOrderBook {
	book := NewOrderBook()
	return SellOrderBook{
		AmountDenom: amountDenom,
		PriceDenom:  priceDenom,
		Book:        &book,
	}
}
