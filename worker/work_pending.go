package worker

import (
	"github.com/shx-project/sphinx/blockchain"
	"sync/atomic"
	"time"
)

type WorkPending struct {
	errored	int32
	Pending 	[]*Work
	InputCh		chan *Work
}

func NewWorkPending() *WorkPending{
	return &WorkPending{errored: 0, Pending:make([]*Work, 0), InputCh:make(chan *Work, 10)}
}

func (p *WorkPending)HaveErr() bool {
	return atomic.LoadInt32(&p.errored) > 0
}

func (p *WorkPending)Add(w *Work) bool {
	if p.HaveErr() {
		return false
	}
	p.InputCh <- w
	return true
}

func (p *WorkPending)Top() *Work {
	if !p.HaveErr() && len(p.Pending) > 0 {
		return p.Pending[len(p.Pending)-1]
	}
	return nil
}

func (p *WorkPending) Empty() bool {
	return len(p.Pending) == 0
}

func (p *WorkPending)SetNoError() {
	atomic.StoreInt32(&p.errored, 0)
}

func (p *WorkPending)pop() *Work {
	if len(p.Pending) > 0 {
		w := p.Pending[0]
		p.Pending = p.Pending[1:]
		return w
	}
	return nil
}

func (p *WorkPending) Run() {
	chain := bc.InstanceBlockChain()
	for true {
		timer := time.NewTimer(time.Millisecond * 500)
		select {
		case work,ok := <-p.InputCh:
			if !ok {
				return
			}
			p.Pending = append(p.Pending, work)

		case <- timer.C:
			if p.HaveErr() {
				for len(p.Pending) > 0 {
					w := p.Pending[0]
					w.WorkEnded(false)
					p.pop()
				}
			}
			if !p.HaveErr() && len(p.Pending) > 0 {
				w := p.Pending[0]
				_, err := chain.WriteBlockAndState(w.Block, w.receipts, w.state)
				if err != nil {
					// enter err mode, not work and receive new work.
					atomic.StoreInt32(&p.errored,1)
				} else {
					w.WorkEnded(true)
					p.pop()
				}
			}
		}
	}
}
