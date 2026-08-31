package app

import "testing"

func TestPeopleAgentQueueKeepsSecondTurn(t *testing.T) {
	resetPeopleAgentQueueForTest()
	first, started, dropped := enqueuePeopleAgentTurn("t1", "m1", "第一句")
	if !started || dropped != "" || first.messageID != "m1" {
		t.Fatalf("first offer started=%v dropped=%q job=%#v", started, dropped, first)
	}
	_, started, dropped = enqueuePeopleAgentTurn("t1", "m2", "第二句")
	if started || dropped != "" {
		t.Fatalf("second offer must queue silently: started=%v dropped=%q", started, dropped)
	}
	second, ok := dequeuePeopleAgentTurn("t1")
	if !ok || second.messageID != "m2" {
		t.Fatalf("second job = %#v ok=%v", second, ok)
	}
	if _, ok := dequeuePeopleAgentTurn("t1"); ok {
		t.Fatal("queue should be empty")
	}
}

func TestPeopleAgentQueueDropsWhenFull(t *testing.T) {
	resetPeopleAgentQueueForTest()
	_, started, _ := enqueuePeopleAgentTurn("t1", "m0", "busy")
	if !started {
		t.Fatal("first must start")
	}
	for i := 0; i < peopleAgentQueueCap; i++ {
		_, started, dropped := enqueuePeopleAgentTurn("t1", "q", "x")
		if started || dropped != "" {
			t.Fatalf("slot %d started=%v dropped=%q", i, started, dropped)
		}
	}
	_, started, dropped := enqueuePeopleAgentTurn("t1", "overflow", "x")
	if started || dropped != peopleAgentDropNotice {
		t.Fatalf("overflow started=%v dropped=%q", started, dropped)
	}
}
