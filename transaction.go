package pfcp

import (
	"net"
	"sync"
	"time"

//	"github.com/free5gc/pfcp/logger"
	"github.com/my-free5gc/pfcp/logger"
	
	"github.com/nctu-ucr/onvmNet"
)

type TransactionType uint8

type TxTable struct {
	m sync.Map // map[uint32]*Transaction
}

func (t *TxTable) Store(sequenceNumber uint32, tx *Transaction) {
	t.m.Store(sequenceNumber, tx)
}

func (t *TxTable) Load(sequenceNumber uint32) (*Transaction, bool) {
	tx, ok := t.m.Load(sequenceNumber)
	if ok {
		return tx.(*Transaction), ok
	}
	return nil, false
}

func (t *TxTable) Delete(sequenceNumber uint32) {
	t.m.Delete(sequenceNumber)
}

const (
	SendingRequest TransactionType = iota
	SendingResponse
)

const (
	NumOfResend                 = 3
	ResendRequestTimeOutPeriod  = 3
	ResendResponseTimeOutPeriod = 15
)

// Transaction - represent the transaction state of pfcp message
type Transaction struct {
	SendMsg        []byte
	SequenceNumber uint32
	MessageType    MessageType
	TxType         TransactionType
	EventChannel   chan EventType
	Conn           *onvmNet.ONVMConn
	DestAddr       *net.UDPAddr
	ConsumerAddr   string
}

// NewTransaction - create pfcp transaction object
func NewTransaction(pfcpMSG Message, binaryMSG []byte, Conn *onvmNet.ONVMConn, DestAddr *net.UDPAddr) (tx *Transaction) {
	tx = &Transaction{
		SendMsg:        binaryMSG,
		SequenceNumber: pfcpMSG.Header.SequenceNumber,
		MessageType:    pfcpMSG.Header.MessageType,
		EventChannel:   make(chan EventType),
		Conn:           Conn,
		DestAddr:       DestAddr,
	}

	if pfcpMSG.IsRequest() {
		tx.TxType = SendingRequest
		//turn onvmaddr to udpaddr
		onvmaddr, ok := Conn.LocalAddr().(*onvmNet.ONVMAddr)
		if !ok {
			/********error handler?****************
			errormsg := fmt.Errorf("In NewTransaction: can't convert onvmaddr to udpaddr")
			fmt.Println(errormsg)
			***************************************/
		}
		udpaddr := onvmaddr.ToUDPAddr()
		tx.ConsumerAddr = udpaddr.String()		
		//tx.ConsumerAddr = Conn.LocalAddr().String()
	} else if pfcpMSG.IsResponse() {
		tx.TxType = SendingResponse
		tx.ConsumerAddr = DestAddr.String()
	}

	logger.PFCPLog.Tracef("New Transaction SEQ[%d] DestAddr[%s]", tx.SequenceNumber, DestAddr.String())
	return
}

func (transaction *Transaction) Start() {

	logger.PFCPLog.Tracef("Start Transaction [%d]\n", transaction.SequenceNumber)

	if transaction.TxType == SendingRequest {
		for iter := 0; iter < NumOfResend; iter++ {
			timer := time.NewTimer(ResendRequestTimeOutPeriod * time.Second)
			//trans udpaddr to onvmaddr
			ONVMDestAddr := onvmNet.UDPToONVMAddr(transaction.DestAddr)
			_, err := transaction.Conn.WriteToONVM(transaction.SendMsg, ONVMDestAddr)
			//_, err := transaction.Conn.WriteToUDP(transaction.SendMsg, transaction.DestAddr)

			if err != nil {
				logger.PFCPLog.Warnf("Request Transaction [%d]: %s\n", transaction.SequenceNumber, err)
				return
			}

			select {
			case event := <-transaction.EventChannel:

				if event == ReceiveValidResponse {
					logger.PFCPLog.Tracef("Request Transaction [%d]: receive valid response\n", transaction.SequenceNumber)
					return
				}
			case <-timer.C:
				logger.PFCPLog.Tracef("Request Transaction [%d]: timeout expire\n", transaction.SequenceNumber)
				logger.PFCPLog.Tracef("Request Transaction [%d]: Resend packet\n", transaction.SequenceNumber)
				continue
			}
		}
	} else if transaction.TxType == SendingResponse {
		//Todo :Implement SendingResponse type of reliable delivery
		timer := time.NewTimer(ResendResponseTimeOutPeriod * time.Second)
		for iter := 0; iter < NumOfResend; iter++ {
			//trans udpaddr to onvmaddr
			ONVMDestAddr := onvmNet.UDPToONVMAddr(transaction.DestAddr)
			_, err := transaction.Conn.WriteToONVM(transaction.SendMsg, ONVMDestAddr)
			//_, err := transaction.Conn.WriteToUDP(transaction.SendMsg, transaction.DestAddr)

			if err != nil {
				logger.PFCPLog.Warnf("Response Transaction [%d]: sending error\n", transaction.SequenceNumber)
				return
			}

			select {
			case event := <-transaction.EventChannel:

				if event == ReceiveResendRequest {
					logger.PFCPLog.Tracef("Response Transaction [%d]: receive resend request\n", transaction.SequenceNumber)
					logger.PFCPLog.Tracef("Response Transaction [%d]: Resend packet\n", transaction.SequenceNumber)
					continue
				}
			case <-timer.C:
				logger.PFCPLog.Tracef("Response Transaction [%d]: timeout expire\n", transaction.SequenceNumber)
				return
			}
		}

	}

}
