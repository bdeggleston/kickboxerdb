package store

import (
	"bufio"
	"time"
)

// enum indicating type of value
type ValueType string

type Value interface {
	// returns the highest level timestamp for this value
	GetTimestamp() time.Time
	GetValueType() ValueType

	Serialize(buf *bufio.Writer) error
	Deserialize(buf *bufio.Reader) error

	// compares 2 Values
	Equal(value Value) bool
}

// an instruction to be executed against
// the store. These objects should be
// considered immutable once instantiated
type Instruction struct {
	Cmd string
	Key string
	Args []string
	Timestamp time.Time
}

// creates a new instruction
func NewInstruction(cmd string, key string, args []string, timestamp time.Time) *Instruction {
	return &Instruction{
		Cmd: cmd,
		Key: key,
		Args: args,
		Timestamp: timestamp,
	}
}

// instruction equality test
func (i *Instruction) Equal(o *Instruction) bool {
	if i.Cmd != o.Cmd { return false }
	if i.Key != o.Key { return false }
	if len(i.Args) != len(o.Args) { return false }
	for n:=0;n<len(i.Args);n++ {
		if i.Args[n] != o.Args[n] { return false}
	}
	if i.Timestamp != o.Timestamp { return false }
	return true
}

func (i *Instruction) Copy() *Instruction {
	newInstr := &Instruction{
		Cmd: i.Cmd,
		Key: i.Key,
		Args: make([]string, len(i.Args)),
		Timestamp: i.Timestamp,
	}
	copy(newInstr.Args, i.Args)
	return newInstr
}

type Store interface {

	Start() error
	Stop() error

	// ----------- queries -----------

	// TODO: remove
	// executes a read instruction against the node's store
	ExecuteRead(cmd string, key string, args []string) (Value, error)

	// TODO: remove
	// executes a write instruction against the node's store
	ExecuteWrite(cmd string, key string, args []string, timestamp time.Time) (Value, error)

	// executes a query instruction against the node's store
	ExecuteQuery(cmd string, key string, args []string, timestamp time.Time) (Value, error)

	// reconciles multiple values and returns instructions for correcting
	// the values on inaccurate nodes
	Reconcile(key string, values map[string] Value) (Value, map[string][]*Instruction, error)

	// TODO: remove
	// determines if the given command is a read command
	IsReadCommand(cmd string) bool

	// TODO: remove
	// determines if the given command is a write command
	IsWriteCommand(cmd string) bool

	// determines if the given command returns a value
	ReturnsValue(cmd string) bool

	// ----------- data import / export -----------

	// serializes a value
	SerializeValue(v Value) ([]byte, error)

	// serializes a value
	DeserializeValue(b []byte) (Value, ValueType, error)

	// returns raw data associated with the given key
	GetRawKey(key string) (Value, error)

	// sets the contents of the given key
	SetRawKey(key string, val Value) error

	// returns all of the keys held by the store
	GetKeys() []string

	// checks if a key exists in the store
	KeyExists(key string) bool
}

