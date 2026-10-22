package firmwarerelay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"
	"google.golang.org/protobuf/proto"

	"github.com/ygelfand/libcountertop/pkg/homeassistant/states"
	"github.com/ygelfand/libcountertop/pkg/tessera/firmware"
)

const answerWait = 15 * time.Second

var blocked = map[string]bool{
	"rotation":            true,
	"standby_after":       true,
	"dark_mode":           true,
	"display_backlight":   true,
	"normal_brightness":   true,
	"standby_brightness":  true,
	"night_brightness":    true,
	"restart":             true,
	"ip_address":          true,
	"psram_free":          true,
	"psram_largest_block": true,
	"last_boot":           true,
	"node_status":         true,
}

const clockPattern = `^([01][0-9]|2[0-3]):[0-5][0-9]$`

type Home interface {
	Relay(ctx context.Context, req *api.HomeassistantActionRequest) ([]byte, error)
}

type Options struct {
	Word    string
	Device  uint32
	Port    int
	Node    func() string
	Zone    func() string
	Home    Home
	States  *states.States
	Changed func()
}

type Relay struct {
	opts Options

	mu       sync.Mutex
	client   *firmware.Client
	entities []esphome.Entity
	actions  []*esphome.Action
	standby  *esphome.Switch
	apply    map[uint32]func(proto.Message)
	calls    map[uint32]chan *api.ExecuteServiceResponse
	next     uint32
	shape    string
}

func New(o Options) *Relay { return &Relay{opts: o} }

func (r *Relay) Name() string { return "tessera firmware relay" }

func (r *Relay) Entities() []esphome.Entity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entities
}

func (r *Relay) Actions() []*esphome.Action {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.actions
}

func (r *Relay) Run(ctx context.Context) error {
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		err := r.session(ctx)
		if ctx.Err() != nil {
			break
		}
		slog.Debug("tessera relay", "err", err)
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 5*time.Second)
	}
	r.forget()
	return nil
}

func (r *Relay) Port() int { return r.opts.Port }

func (r *Relay) Standby() bool {
	r.mu.Lock()
	s := r.standby
	r.mu.Unlock()
	return s != nil && s.Get()
}

func (r *Relay) forget() {
	r.mu.Lock()
	had := len(r.entities) > 0 || len(r.actions) > 0
	r.client, r.entities, r.actions, r.standby, r.shape = nil, nil, nil, nil, ""
	r.mu.Unlock()
	if had {
		r.opts.Changed()
	}
}

func (r *Relay) session(ctx context.Context) error {
	c, err := firmware.Dial(ctx, r.opts.Node(), r.opts.Port)
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	defer c.Close()
	if err := c.Send(&api.ListEntitiesRequest{}); err != nil {
		return err
	}
	var listed []proto.Message
	for {
		m, err := c.Recv()
		if err != nil {
			return err
		}
		if _, done := m.(*api.ListEntitiesDoneResponse); done {
			break
		}
		listed = append(listed, m)
	}
	r.mirror(c, listed)
	for _, m := range []proto.Message{
		&api.SubscribeStatesRequest{},
		&api.SubscribeHomeassistantServicesRequest{},
		&api.SubscribeHomeAssistantStatesRequest{},
	} {
		if err := c.Send(m); err != nil {
			return err
		}
	}
	var following []func()
	defer func() {
		for _, cancel := range following {
			cancel()
		}
	}()
	for {
		m, err := c.Recv()
		if err != nil {
			return err
		}
		if key, ok := stateKey(m); ok {
			r.state(key, m)
			continue
		}
		switch m := m.(type) {
		case *api.ExecuteServiceResponse:
			r.answered(m)
		case *api.HomeassistantActionRequest:
			go r.perform(ctx, c, m)
		case *api.SubscribeHomeAssistantStateResponse:
			following = append(following, r.follow(c, m))
		case *api.GetTimeRequest:
			err = c.Send(r.now())
		case *api.PingRequest:
			err = c.Send(&api.PingResponse{})
		case *api.DisconnectRequest:
			_ = c.Send(&api.DisconnectResponse{})
			return errors.New("tessera: helper said goodbye")
		}
		if err != nil {
			return err
		}
	}
}

func (r *Relay) mirror(c *firmware.Client, listed []proto.Message) {
	var (
		entities []esphome.Entity
		acts     []*esphome.Action
		standby  *esphome.Switch
		apply    = map[uint32]func(proto.Message){}
		shape    []string
	)
	add := func(e esphome.Entity, kind, id string) {
		entities = append(entities, e)
		shape = append(shape, kind+":"+id)
	}
	for _, m := range listed {
		if id, ok := listedID(m); ok && blocked[id] {
			continue
		}
		switch m := m.(type) {
		case *api.ListEntitiesTextSensorResponse:
			id := slug(m.Name)
			t := &esphome.TextSensor{Base: r.base(id, m.Name, m.Icon, m.EntityCategory)}
			if id == "device_name" {
				t.Set(r.opts.Node() + "-" + r.opts.Word)
			} else {
				apply[m.Key] = func(s proto.Message) { t.Set(s.(*api.TextSensorStateResponse).State) }
			}
			add(t, "t", id)
		case *api.ListEntitiesTextResponse:
			id := slug(m.Name)
			key := m.Key
			t := &esphome.Text{
				Base:      r.base(id, m.Name, m.Icon, m.EntityCategory),
				MinLength: m.MinLength,
				MaxLength: m.MaxLength,
				Pattern:   m.Pattern,
				Mode:      esphome.TextPlain,
			}
			t.OnCommand = func(v string) { _ = c.Send(&api.TextCommandRequest{Key: key, State: v}) }
			apply[m.Key] = func(s proto.Message) { t.Set(s.(*api.TextStateResponse).State) }
			add(t, "x", id)
		case *api.ListEntitiesSwitchResponse:
			id := slug(m.Name)
			key := m.Key
			s := &esphome.Switch{Base: r.base(id, m.Name, m.Icon, m.EntityCategory)}
			s.OnCommand = func(on bool) { _ = c.Send(&api.SwitchCommandRequest{Key: key, State: on}) }
			apply[m.Key] = func(st proto.Message) { s.Set(st.(*api.SwitchStateResponse).State) }
			if id == "auto_standby" {
				standby = s
			}
			add(s, "s", id)
		case *api.ListEntitiesNumberResponse:
			id := slug(m.Name)
			key := m.Key
			n := &esphome.Number{
				Base: r.base(id, m.Name, m.Icon, m.EntityCategory),
				Min:  float64(m.MinValue),
				Max:  float64(m.MaxValue),
				Step: float64(m.Step),
				Unit: m.UnitOfMeasurement,
				Mode: m.Mode,
			}
			n.OnCommand = func(v float32) { _ = c.Send(&api.NumberCommandRequest{Key: key, State: v}) }
			apply[m.Key] = func(st proto.Message) {
				if s := st.(*api.NumberStateResponse); !s.MissingState {
					n.Set(s.State)
				}
			}
			add(n, "n", id)
		case *api.ListEntitiesTimeResponse:
			id := slug(m.Name)
			key := m.Key
			t := &esphome.Text{
				Base:      r.base(id, m.Name, m.Icon, m.EntityCategory),
				MinLength: 5,
				MaxLength: 5,
				Pattern:   clockPattern,
				Mode:      esphome.TextPlain,
			}
			t.OnCommand = func(v string) {
				var hour, minute uint32
				if _, err := fmt.Sscanf(
					v,
					"%d:%d",
					&hour,
					&minute,
				); err == nil && hour < 24 &&
					minute < 60 {
					_ = c.Send(&api.TimeCommandRequest{Key: key, Hour: hour, Minute: minute})
				}
			}
			apply[m.Key] = func(st proto.Message) {
				if s := st.(*api.TimeStateResponse); !s.MissingState {
					t.Set(fmt.Sprintf("%02d:%02d", s.Hour, s.Minute))
				}
			}
			add(t, "c", id)
		case *api.ListEntitiesButtonResponse:
			id := slug(m.Name)
			key := m.Key
			b := &esphome.Button{Base: r.base(id, m.Name, m.Icon, m.EntityCategory)}
			b.OnPress = func() { _ = c.Send(&api.ButtonCommandRequest{Key: key}) }
			add(b, "b", id)
		case *api.ListEntitiesSensorResponse:
			id := slug(m.Name)
			n := &esphome.Sensor{
				Base:        r.base(id, m.Name, m.Icon, m.EntityCategory),
				Unit:        m.UnitOfMeasurement,
				DeviceClass: m.DeviceClass,
				StateClass:  esphome.SensorStateClass(m.StateClass),
				Decimals:    m.AccuracyDecimals,
			}
			apply[m.Key] = func(st proto.Message) {
				if s := st.(*api.SensorStateResponse); !s.MissingState {
					n.Set(s.State)
				}
			}
			add(n, "r", id)
		case *api.ListEntitiesBinarySensorResponse:
			id := slug(m.Name)
			b := &esphome.BinarySensor{
				Base:        r.base(id, m.Name, m.Icon, m.EntityCategory),
				DeviceClass: m.DeviceClass,
			}
			apply[m.Key] = func(st proto.Message) { b.Set(st.(*api.BinarySensorStateResponse).State) }
			add(b, "y", id)
		case *api.ListEntitiesSelectResponse:
			id := slug(m.Name)
			key := m.Key
			o := &esphome.Select{
				Base:    r.base(id, m.Name, m.Icon, m.EntityCategory),
				Options: m.Options,
			}
			o.OnCommand = func(v string) { _ = c.Send(&api.SelectCommandRequest{Key: key, State: v}) }
			apply[m.Key] = func(st proto.Message) {
				if s := st.(*api.SelectStateResponse); !s.MissingState {
					o.Set(s.State)
				}
			}
			add(o, "o", id)
		case *api.ListEntitiesServicesResponse:
			if blocked[m.Name] {
				continue
			}
			acts = append(acts, r.action(m))
			shape = append(shape, "a:"+m.Name)
		}
	}
	r.mu.Lock()
	changed := strings.Join(shape, ",") != r.shape
	r.client, r.entities, r.actions, r.standby = c, entities, acts, standby
	r.apply, r.shape = apply, strings.Join(shape, ",")
	r.calls = map[uint32]chan *api.ExecuteServiceResponse{}
	r.mu.Unlock()
	if changed {
		r.opts.Changed()
	}
}

func (r *Relay) state(key uint32, m proto.Message) {
	r.mu.Lock()
	apply := r.apply[key]
	r.mu.Unlock()
	if apply != nil {
		apply(m)
	}
}

func listedID(m proto.Message) (string, bool) {
	n, ok := m.(interface{ GetName() string })
	if !ok {
		return "", false
	}
	if _, service := m.(*api.ListEntitiesServicesResponse); service {
		return "", false
	}
	return slug(n.GetName()), true
}

func stateKey(m proto.Message) (uint32, bool) {
	switch m := m.(type) {
	case *api.TextSensorStateResponse:
		return m.Key, true
	case *api.TextStateResponse:
		return m.Key, true
	case *api.SwitchStateResponse:
		return m.Key, true
	case *api.NumberStateResponse:
		return m.Key, true
	case *api.TimeStateResponse:
		return m.Key, true
	case *api.SensorStateResponse:
		return m.Key, true
	case *api.BinarySensorStateResponse:
		return m.Key, true
	case *api.SelectStateResponse:
		return m.Key, true
	}
	return 0, false
}

func (r *Relay) base(id, name, icon string, category esphome.EntityCategory) esphome.Base {
	return esphome.Base{
		ObjectID: "tessera_" + r.opts.Word + "_" + id,
		Name:     name,
		Icon:     icon,
		Category: category,
		DeviceID: r.opts.Device,
	}
}

func (r *Relay) action(m *api.ListEntitiesServicesResponse) *esphome.Action {
	args := make([]esphome.Arg, 0, len(m.Args))
	for _, a := range m.Args {
		args = append(args, esphome.Arg{Name: a.Name, Type: argType(a.Type)})
	}
	key := m.Key
	answers := m.SupportsResponse != api.SupportsResponseType_SUPPORTS_RESPONSE_NONE
	return &esphome.Action{
		Name:    r.opts.Word + "_" + m.Name,
		Args:    args,
		Answers: answers,
		Run: func(call esphome.Call) (any, error) {
			return r.execute(key, args, call, answers)
		},
	}
}

func (r *Relay) execute(
	key uint32,
	args []esphome.Arg,
	call esphome.Call,
	answers bool,
) (any, error) {
	values := make([]*api.ExecuteServiceArgument, 0, len(args))
	for _, a := range args {
		switch a.Type {
		case esphome.ArgInt:
			values = append(values, &api.ExecuteServiceArgument{Int_: int32(call.Int(a.Name))})
		case esphome.ArgFloat:
			values = append(values, &api.ExecuteServiceArgument{Float_: call.Float(a.Name)})
		case esphome.ArgBool:
			values = append(values, &api.ExecuteServiceArgument{Bool_: call.Bool(a.Name)})
		default:
			values = append(values, &api.ExecuteServiceArgument{String_: call.String(a.Name)})
		}
	}
	r.mu.Lock()
	c := r.client
	if c == nil {
		r.mu.Unlock()
		return nil, errors.New("tessera: the screen is not running")
	}
	r.next++
	id := r.next
	ch := make(chan *api.ExecuteServiceResponse, 1)
	if answers {
		r.calls[id] = ch
	}
	r.mu.Unlock()
	req := &api.ExecuteServiceRequest{Key: key, Args: values}
	if answers {
		req.CallId, req.ReturnResponse = id, true
	}
	if err := c.Send(req); err != nil || !answers {
		return nil, err
	}
	defer func() {
		r.mu.Lock()
		delete(r.calls, id)
		r.mu.Unlock()
	}()
	select {
	case m := <-ch:
		if !m.Success {
			return nil, fmt.Errorf("tessera: %s", m.ErrorMessage)
		}
		if len(m.ResponseData) == 0 {
			return nil, nil
		}
		var answer any
		if err := json.Unmarshal(m.ResponseData, &answer); err != nil {
			return nil, fmt.Errorf("tessera: answer: %w", err)
		}
		return answer, nil
	case <-time.After(answerWait):
		return nil, errors.New("tessera: the screen did not answer")
	}
}

func (r *Relay) answered(m *api.ExecuteServiceResponse) {
	r.mu.Lock()
	ch := r.calls[m.CallId]
	r.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (r *Relay) perform(
	ctx context.Context,
	c *firmware.Client,
	m *api.HomeassistantActionRequest,
) {
	ctx, cancel := context.WithTimeout(ctx, answerWait)
	defer cancel()
	data, err := r.opts.Home.Relay(ctx, m)
	if m.CallId == 0 {
		if err != nil {
			slog.Debug("tessera action", "action", m.Service, "err", err)
		}
		return
	}
	reply := &api.HomeassistantActionResponse{
		CallId:       m.CallId,
		Success:      err == nil,
		ResponseData: data,
	}
	if err != nil {
		reply.ErrorMessage = err.Error()
	}
	if err := c.Send(reply); err != nil {
		slog.Debug("tessera action reply", "err", err)
	}
}

func (r *Relay) follow(c *firmware.Client, m *api.SubscribeHomeAssistantStateResponse) func() {
	v := r.opts.States.Follow(m.EntityId, m.Attribute)
	tell := func(state string) {
		_ = c.Send(&api.HomeAssistantStateResponse{
			EntityId:  m.EntityId,
			Attribute: m.Attribute,
			State:     state,
		})
	}
	if state, ok := v.Get(); ok {
		tell(state)
	}
	return v.Changed.Listen(tell)
}

func (r *Relay) now() *api.GetTimeResponse {
	zone := r.opts.Zone()
	reply := &api.GetTimeResponse{EpochSeconds: uint32(time.Now().Unix()), Timezone: zone}
	if tz, ok := parseZone(zone); ok {
		reply.ParsedTimezone = tz
	} else if zone != "" {
		slog.Warn("tessera: time zone not understood", "zone", zone)
	}
	return reply
}

func argType(t api.ServiceArgType) esphome.ArgType {
	switch t {
	case api.ServiceArgType_SERVICE_ARG_TYPE_INT:
		return esphome.ArgInt
	case api.ServiceArgType_SERVICE_ARG_TYPE_FLOAT:
		return esphome.ArgFloat
	case api.ServiceArgType_SERVICE_ARG_TYPE_BOOL:
		return esphome.ArgBool
	}
	return esphome.ArgString
}

func slug(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}
