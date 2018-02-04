package main

import (
	"gobot.io/x/gobot"
	"gobot.io/x/gobot/platforms/neurosky"
	"fmt"
	"github.com/VividCortex/ewma"
	lane "gopkg.in/oleiade/lane.v1"
	"github.com/ctcpip/notifize"
	"time"
	"path"
	"os/exec"
)

var (
	sum int
	duration = 50 * time.Millisecond
	doubleClenchTimeout = 8 * time.Second
	historyLength = 25
	emgThresholdMax = 10.0
	emgThresholdMin = -200.0

	emgEventTypeReflexBlinkThresholdMax = 10.0
	emgEventTypeReflexBlinkThresholdMin = -10.0

	emgEventTypeClenchThresholdMax = -10.0
	emgEventTypeClenchThresholdMin = -70.0

	emgEventTypeVoluntaryBlinkThresholdMax = -70.0
	emgEventTypeVoluntaryBlinkThresholdMin = -200.0

	emgSynthesizedEventDoubleClenchSpacingMax = 700 * time.Millisecond
	emgSynthesizedEventDoubleClenchSpacingMin = 250 * time.Millisecond

	shouldTriggerEvents = false
)


const (
	applescript_directory = "/Users/genslerj/Documents/gopath/src/github.com/jgensler8/mindwave/applescripts"

	EMGEventTypeNone = "none"
	EMGEventTypeVoluntaryBlink = "vblink"
	EMGEventTypeReflexBlink = "rblink"
	EMGEventTypeClench = "clench"

	EMGSynthesizedEventDoubleClench = "dclench"
)

type EMGRawValue struct {
	Value float64
}

type EMGEvent struct {
	RawValue EMGRawValue
	Type string
}

type EMGEventHistory struct {
	History *lane.Deque
	EventSpacing time.Duration
}

func IsNoneEvent(raw EMGRawValue) (bool) {
	return raw.Value > emgThresholdMax || raw.Value < emgThresholdMin
}

func IsReflexBlinkEvent(raw EMGRawValue) (bool) {
	return raw.Value < emgEventTypeReflexBlinkThresholdMax && raw.Value > emgEventTypeReflexBlinkThresholdMin
}

func IsVoluntaryBlinkEvent(raw EMGRawValue) (bool) {
	return raw.Value < emgEventTypeVoluntaryBlinkThresholdMax && raw.Value > emgEventTypeVoluntaryBlinkThresholdMin
}

func IsClenchEvent(raw EMGRawValue) (bool) {
	return raw.Value < emgEventTypeClenchThresholdMax && raw.Value > emgEventTypeClenchThresholdMin
}

func IsDoubleClenchEvent(h *EMGEventHistory) (bool) {
	foundDoubleClench := false
	if h.History.First().(EMGEvent).Type == EMGEventTypeClench {
		spacing := 0 * time.Millisecond

		for i := 0; i < h.History.Size(); i++ {
			el := h.History.Shift().(EMGEvent)

			// if another event is clench and we are in the range
			if el.Type == EMGEventTypeClench && spacing > emgSynthesizedEventDoubleClenchSpacingMin && spacing < emgSynthesizedEventDoubleClenchSpacingMax {
				foundDoubleClench = true
			}

			spacing = spacing + h.EventSpacing

			h.History.Append(el)
		}
	}
	return foundDoubleClench
}

func NewEMGEventHistory(historyLength int) (*EMGEventHistory) {
	h := &EMGEventHistory{
		History: lane.NewCappedDeque(historyLength),
		EventSpacing: duration,
	}
	for i := 0; i < historyLength; i++ {
		h.History.Append(EMGEvent{
			Type: EMGEventTypeNone,
		})
	}
	return h
}

func (h EMGEventHistory) ToString() (string) {
	s := ""
	for i := 0; i < h.History.Size(); i++ {
		el := h.History.Shift()
		s = s + " " + fmt.Sprintf("%v", el)
		h.History.Append(el)
	}
	return s
}

func (h EMGEventHistory) Normalize() {
	for i := 0; i < h.History.Size(); i++ {
		h.History.Shift()
		h.History.Append(EMGEvent{
			Type: EMGEventTypeNone,
		})
	}
}

func ExecApplescript(scriptNum int) {
	OSASCRIPT := "osascript"
	p := path.Join(applescript_directory, fmt.Sprintf("workspace_%d.applescript", scriptNum))
	fmt.Printf("Execing %s", p)
	cmd := exec.Command(OSASCRIPT, p)
	err := cmd.Start()
	if err != nil {
		fmt.Printf("%s")
	}
}

// EMGEventTypeDecorator receives an EMGEvent with a raw value and set the EMGEventType
func EMGEventTypeDecorator(emgChan chan EMGEvent) (chan EMGEvent) {
	history := NewEMGEventHistory(historyLength)
	clenchCount := 0

	for {
		emgEvent := <-emgChan

		// Evaluate Event
		if IsNoneEvent(emgEvent.RawValue) {
			emgEvent.Type = EMGEventTypeNone
			//fmt.Println(EMGEventTypeNone)
		} else if IsReflexBlinkEvent(emgEvent.RawValue) {
			emgEvent.Type = EMGEventTypeReflexBlink
			//fmt.Println(EMGEventTypeReflexBlink)
		} else if IsVoluntaryBlinkEvent(emgEvent.RawValue) {
			emgEvent.Type = EMGEventTypeVoluntaryBlink
			//fmt.Println(EMGEventTypeVoluntaryBlink)
		} else if IsClenchEvent(emgEvent.RawValue) {
			emgEvent.Type = EMGEventTypeClench
			fmt.Println(EMGEventTypeClench)
			if shouldTriggerEvents {
				clenchCount = clenchCount + 1
				notifize.Display("Clench", fmt.Sprintf("%d", clenchCount), false, "")
				ExecApplescript(clenchCount)
			}
		}

		// Store history
		history.History.Pop()
		history.History.Prepend(emgEvent)

		// Evaluate Synthetic Events
		if IsDoubleClenchEvent(history) {
			fmt.Println(EMGSynthesizedEventDoubleClench)
			history.Normalize()

			shouldTriggerEvents = true
			shouldStopTriggerTimer := time.NewTimer(doubleClenchTimeout)
			go func() {
				<-shouldStopTriggerTimer.C
				shouldTriggerEvents = false
				clenchCount = 0
			}()
			clenchCount = 1
			ExecApplescript(clenchCount)
			notifize.Display("Double Clench", "Reset to workspace 1", false, "")
		}

		//fmt.Println("~~~~~")
	}
}


func main() {
	adaptor := neurosky.NewAdaptor("/dev/tty.MindWaveMobile-DevA")
	neuro := neurosky.NewDriver(adaptor)

	a := ewma.NewMovingAverage(30)

	emvEvents := make(chan EMGEvent)
	go EMGEventTypeDecorator(emvEvents)

	ticker := time.NewTicker(duration)

	//chord = ChordBuilder()
	//  .When(EMGSynthesizedEventDoubleClench)
	//  .Every(EMGEventTypeClench, "after", IncreateWorkspace)
	//  .Stop(time.Second * 4)

	go func() {
		for range ticker.C {
			//fmt.Printf("~~~~~~~~~~~~ %d operations per %s ::: %v\n", sum, duration.String(), a.Value())
			sum = 0
			if a.Value() != 0 {
				emvEvents <- EMGEvent{ RawValue: EMGRawValue{ Value: a.Value() } }
			}
		}
	}()

	work := func() {
		neuro.On(neuro.Event("wave"), func(data interface{}) {
			sum = sum + 1
			var x int16
			x = data.(int16)
			a.Add(float64(x))
		})
	}

	robot := gobot.NewRobot("brainBot",
		[]gobot.Connection{adaptor},
		[]gobot.Device{neuro},
		work,
	)

	robot.Start()
}