package protofuzz

import (
	"fmt"
	"os"

	"github.com/emicklei/proto"
)

type pf struct {
	types []string
	togen map[string]struct{}
	vals  map[string]any
	gen   *valGen
}

type valGen struct {
	proto.Visitor
}

func Run(file, out string, types []string) error {
	if len(types) == 1 && len(types[0]) == 0 {
		types = nil
	}
	togen := make(map[string]struct{}, len(types))
	vals := make(map[string]any, len(types))
	for _, t := range types {
		togen[t] = struct{}{}
	}
	fuzz := pf{
		types: types,
		togen: togen,
		vals:  vals,
		gen:   &valGen{},
	}
	if err := fuzz.Parse(file); err != nil {
		return err
	}
	return nil
}

func (p *pf) Parse(file string) error {
	reader, err := os.Open(file)
	if err != nil {
		return err
	}
	defer reader.Close()
	parser := proto.NewParser(reader)
	defs, err := parser.Parse()
	if err != nil {
		return err
	}
	proto.Walk(defs, proto.WithMessage(p.generateMessage))
	return nil
}

func (p *pf) generateMessage(m *proto.Message) {
	if len(p.types) == 0 {
		p.togen[m.Name] = struct{}{}
		p.vals[m.Name] = nil
	}
	if _, ok := p.togen[m.Name]; !ok {
		fmt.Printf("%s skipped", m.Name)
		return
	}
	fmt.Println(m.Name)
	for _, e := range m.Elements {
		e.Accept(p.gen)
	}
}

func (v *valGen) VisitOption(o *proto.Option) {
	fmt.Println(o.Name)
}

func (v *valGen) VisitMessage(m *proto.Message) {
	fmt.Println(m.Name)
}

func (v *valGen) VisitOneof(o *proto.Oneof) {
	fmt.Println(o.Name)
}

func (v *valGen) VisitOneofField(o *proto.OneOfField) {
	fmt.Println(o.Name)
}

func (v *valGen) VisitEnum(e *proto.Enum) {
	fmt.Println(e.Name)
}

func (v *valGen) VisitEnumField(i *proto.EnumField) {
	fmt.Println(i.Name)
}

func (v *valGen) VisitMapField(i *proto.MapField) {
	fmt.Println(i.Name)
}

func (v *valGen) VisitNormalField(i *proto.NormalField) {
	fmt.Println(i.Name)
}

func (v *valGen) VisitReserved(r *proto.Reserved) {
	fmt.Printf("%#v\n", r.FieldNames)
}
