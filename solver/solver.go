// Package solver implements the canonical solution for the challenge.
// This is used as a baseline when evaluating the user's solution.
//
// The implementation is inefficient because it relies a lot on pointers and memory allocations.
// This is because this solution was built up experimentally to see what works and what does not.
// It should be possible to build a more efficient version once sdin is stable.
package solver

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"maps"
	"os"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ypsu/sdin"
	"github.com/ypsu/sdin/tokens"
	"github.com/ypsu/textar"
)

type solver struct{}

func (s *solver) Unescape(p string) (string, error) {
	if p == "" || p[0] != '"' {
		return "ERROR\nunescape.NotADoubleQuoteString\n", nil
	}
	q, err := strconv.Unquote(p)
	if err != nil {
		return "", fmt.Errorf("unescape.Unquote: %v", err)
	}
	return q, nil
}

func (s *solver) Tokenize(input string) (string, error) {
	p, w := input, &strings.Builder{}
	ts, err := tokens.Tokenize(p)
	if err != nil {
		return "", err
	}
	for _, t := range ts {
		s := p[t.Start:t.End]
		fmt.Fprintf(w, "%q\n{Line:%d Length:%d Start:%d}\n", s, t.Line, t.End-t.Start, t.Start)
	}
	return w.String(), nil
}

func debugPrint(w *strings.Builder, n *sdin.Node, depth int) {
	w.WriteString(strings.Repeat(". ", depth))
	w.WriteByte('{')
	if len(n.Name) > 0 {
		fmt.Fprintf(w, "Name:%q ", n.Name)
	}
	fmt.Fprintf(w, "Value:%q", n.Value.Repr())
	if n.Line > 0 {
		fmt.Fprintf(w, " Line:%d", n.Line)
	}
	if len(n.Children) != 0 {
		fmt.Fprintf(w, " Children:%d", len(n.Children))
	}
	if len(n.Pre) > 0 {
		fmt.Fprintf(w, " Pre:%q", n.Pre)
	}
	if len(n.Post) > 0 {
		fmt.Fprintf(w, " Post:%q", n.Post)
	}
	if len(n.PreClose) > 0 {
		fmt.Fprintf(w, " PreClose:%q", n.PreClose)
	}
	if len(n.PostClose) > 0 {
		fmt.Fprintf(w, " PostClose:%q", n.PostClose)
	}
	w.WriteString("}\n")
	for _, ch := range n.Children {
		debugPrint(w, ch, depth+1)
	}
}

var forms = map[string]func(n, vars *sdin.Node) (*sdin.Node, error){
	"stdemo.Env": func(n, _ *sdin.Node) (*sdin.Node, error) {
		if len(n.Children) != 1 && len(n.Children) != 2 {
			return nil, fmt.Errorf("stdemo.BadEnvInvocation: usage is stdemo.Env(envvarIdentifier default:defaultString)")
		}
		if len(n.Children) == 2 && n.Children[1].Name != "default" {
			return nil, fmt.Errorf("stdemo.BadEnvInvocation arg=%s: the second argument, if given, must be `default`", n.Children[1].Name)
		}
		if n.Children[0].Name != "" {
			return nil, fmt.Errorf("stdemo.BadEnvInvocation arg=%s: the first argument must be unnamed", n.Children[0].Name)
		}
		if len(n.Children) == 2 && !n.Children[1].Kind().String() {
			return nil, fmt.Errorf("stdemo.BadEnvDefaultType got=%s: default's type must be string", n.Children[1].Value.Repr())
		}
		envvar, err := n.Children[0].Value.As[sdin.Identifier]()
		if err != nil {
			return nil, fmt.Errorf("stdemo.EnvvarAsIdentifier: %v", err)
		}
		value, ok := os.LookupEnv(string(envvar))
		if ok {
			return sdin.NewNode("", value), nil
		}
		if len(n.Children) == 2 {
			return &sdin.Node{Value: n.Children[1].Value}, nil
		}
		return nil, fmt.Errorf("stdemo.EnvvarNotFound var=%s", envvar)
	},

	"stdemo.Echo": func(n, _ *sdin.Node) (*sdin.Node, error) {
		if len(n.Children) != 1 {
			return nil, fmt.Errorf("stdemo.BadEchoInvocation: usage is stdemo.Echo(sdinString)")
		}
		p, err := n.Children[0].Value.As[string]()
		if err != nil {
			return nil, fmt.Errorf("stdemo.EchoParamAsString: %v", err)
		}
		nn, err := sdin.Parse(sdin.TFlatList(), p)
		if err != nil {
			return nil, fmt.Errorf("stdemo.ParseEchoParam: %v", err)
		}
		if len(nn.Children) != 1 {
			return nil, fmt.Errorf("stdemo.NotSingleValue childrenlen=%d", len(nn.Children))
		}
		return nn.Children[0], nil
	},

	"stdemo.Var": func(n, vars *sdin.Node) (*sdin.Node, error) {
		if len(n.Children) != 1 {
			return nil, fmt.Errorf("stdemo.BadVarInvocation: stdemo.Var() needs exactly one identifier arg")
		}
		v, err := n.Children[0].Value.As[sdin.Identifier]()
		if err != nil {
			return nil, fmt.Errorf("stdemo.BadVarType: stdemo.Var() needs exactly one identifier arg")
		}
		vn := vars.Get(string(v))
		if vn == nil {
			return nil, fmt.Errorf("stdemo.NonexistentVar var=%s: var doesn't exist in the vars struct", v)
		}
		return sdin.NewNode("", vn), nil
	},

	"stdemo.Merge": func(n, _ *sdin.Node) (*sdin.Node, error) {
		if len(n.Children) != 2 {
			return nil, fmt.Errorf("stdemo.BadMergeInvocation: stdemo.Merge() needs exactly two object args")
		}
		dst, src := n.Children[0], n.Children[1]
		if !dst.Kind().Struct() || !src.Kind().Struct() {
			return nil, fmt.Errorf("stdemo.BadMergeArgs lt=%q rt=%q: stdemo.Merge() needs exactly two object args", dst.Value.Repr(), src.Value.Repr())
		}
		for _, ch := range src.Children {
			if dst.Get(ch.Name) != nil {
				return nil, fmt.Errorf("stdemo.DstFieldExists field=%s: merge doesn't override fields", ch.Name)
			}
			dst.Children = append(dst.Children, ch)
		}
		return dst, nil
	},
}

func (s *solver) Ast(p string) (string, error) {
	n, err := sdin.Unmarshal[*sdin.Node](p)
	if err != nil {
		return "", fmt.Errorf("ast.Parse: %v", err)
	}
	w := &strings.Builder{}
	debugPrint(w, n, 0)
	return w.String(), nil
}

func (s *solver) Tojson(p string) (string, error) {
	n, err := sdin.Unmarshal[*sdin.Node](p)
	if err != nil {
		return "", fmt.Errorf("tojson.Parse: %v", err)
	}
	errs := sdin.Sanitizer{}.Run(n)
	if errs != nil {
		w := &strings.Builder{}
		w.WriteString("ERROR\n")
		for _, err := range errs {
			w.WriteString(err.Error())
			w.WriteByte('\n')
		}
		return w.String(), nil
	}
	vars := n.Get("Vars")
	err = n.Replace(func(n *sdin.Node) (*sdin.Node, error) {
		if st, err := n.As[sdin.StringToken](); err == nil && st.Tag() == "stdemo.RemoveComments" { // on success
			return sdin.NewNode("", removeComments(st.Data)), nil
		}
		if fn, ok := forms[n.Kind().FormName()]; ok {
			return fn(n, vars)
		}
		return n, nil
	})
	if err != nil {
		return "", fmt.Errorf("tojson.ReplaceForms: %v", err)
	}
	n.Children = slices.DeleteFunc(n.Children, func(ch *sdin.Node) bool {
		for _, a := range ch.Directives() {
			if a.Kind().FormName() == "stdemo.SkipJSONEmit" {
				return true
			}
		}
		return false
	})
	r, err := n.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("tojson.MarshalJSON: %v", err)
	}
	r = append(r, '\n')
	return string(r), nil
}

func printHTML(n *sdin.Node, w *strings.Builder) error {
	if n.Kind().String() {
		s, err := n.As[string]()
		if err != nil {
			return fmt.Errorf("forms.ParseHTMLString line=%d: %v", n.Line, err)
		}
		w.WriteString(html.EscapeString(s))
		return nil
	}
	if !n.Kind().Form() {
		return fmt.Errorf("forms.UnsupportedTokenForHTML line=%d token=%s", n.Line, n.Value.Repr())
	}
	if n.Value.Repr() != "stdemo.HTML(" {
		w.WriteByte('<')
		w.WriteString(n.Value.Repr()[:len(n.Value.Repr())-1])
		for _, ch := range n.Children {
			if ch.Name != "" {
				tok, err := ch.As[sdin.BareToken]()
				s := string(tok)
				if err != nil {
					s, err = ch.As[string]()
					if err != nil {
						return fmt.Errorf("forms.UnsupportedAttributeForHTML line=%d name=%s token=%s", ch.Line, ch.Name, ch.Value.Repr())
					}
				}
				fmt.Fprintf(w, " %s=\"%s\"", ch.Name, html.EscapeString(s))
			}
		}
		w.WriteByte('>')
	}
	for _, ch := range n.Children {
		if ch.Name == "" {
			if err := printHTML(ch, w); err != nil {
				return err
			}
		}
	}
	if n.Value.Repr() != "stdemo.HTML(" && n.Value.Repr() != "input(" {
		w.WriteString("</")
		w.WriteString(n.Value.Repr()[:len(n.Value.Repr())-1])
		w.WriteByte('>')
	}
	return nil
}

func writeIndent(w *strings.Builder, indent int) {
	for range indent {
		w.WriteByte('\t')
	}
}

func printGoImports(n *sdin.Node, w *strings.Builder) error {
	var unnamed, named, blank []string
	for _, ch := range n.Children {
		pkg, err := ch.As[string]()
		if err != nil {
			return fmt.Errorf("forms.ParseImport line=%d: %v", ch.Line, err)
		}
		pkg = fmt.Sprintf("%q", pkg)
		if ch.Name == "_" {
			if strings.HasPrefix(ch.Post, "#") {
				pkg += " //" + strings.TrimRight(ch.Post[1:], "\n")
			}
			blank = append(blank, "\t_ "+pkg)
		} else if ch.Name == "" {
			unnamed = append(unnamed, "\t"+pkg)
		} else {
			named = append(named, "\t"+ch.Name+" "+pkg)
		}
	}
	slices.Sort(named)
	slices.Sort(unnamed)
	slices.Sort(blank)

	w.WriteString("import(\n")
	for _, line := range unnamed {
		w.WriteString(line)
		w.WriteByte('\n')
	}
	if len(unnamed) > 0 && len(named) > 0 {
		w.WriteByte('\n')
	}
	for _, line := range named {
		w.WriteString(line)
		w.WriteByte('\n')
	}
	if (len(unnamed) > 0 || len(named) > 0) && len(blank) > 0 {
		w.WriteByte('\n')
	}
	for _, line := range blank {
		w.WriteString(line)
		w.WriteByte('\n')
	}
	w.WriteString(")")

	return nil
}

func (s *solver) Serialize(p string) (string, error) {
	n, err := sdin.Unmarshal[*sdin.Node](p)
	if err != nil {
		return "", fmt.Errorf("serialize.Parse: %v", err)
	}
	errs := sdin.Sanitizer{}.Run(n)
	if errs != nil {
		w := &strings.Builder{}
		w.WriteString("ERROR\n")
		for _, err := range errs {
			w.WriteString(err.Error())
			w.WriteByte('\n')
		}
		return w.String(), nil
	}
	return n.String(), nil
}

func (s *solver) Forms(p string) (string, error) {
	n, err := sdin.Parse(sdin.TFlatList(), p)
	if err != nil {
		return "", fmt.Errorf("forms.Parse: %v", err)
	}
	w := &strings.Builder{}
	for _, ch := range n.Children {
		switch ch.Value {
		case sdin.TForm("stdemo.Print"):
			if len(ch.Children) != 1 || !ch.Children[0].Kind().String() {
				return "", fmt.Errorf("forms.InvalidPrintForm")
			}
			s, err := ch.Children[0].As[sdin.StringToken]()
			if err != nil {
				return "", fmt.Errorf("forms.InvalidPrintArg: %v", err)
			}
			if s.Tag() == "stdemo.RemoveComments" {
				return removeComments(s.Data), nil
			}
			return s.Data, nil
		case sdin.TForm("stdemo.HTML"):
			if err := printHTML(ch, w); err != nil {
				return "", fmt.Errorf("forms.PrintHTML: %v", err)
			}
		case sdin.TForm("stdemo.GoImport"):
			if err := printGoImports(ch, w); err != nil {
				return "", fmt.Errorf("forms.PrintGoImports: %v", err)
			}
		case sdin.TForm("stdemo.Groups"):
			if err := expandGroups(ch, w); err != nil {
				return "", fmt.Errorf("forms.ExpandGroups: %v", err)
			}
		default:
			return "", fmt.Errorf("forms.UnsupportedForm line=%d form=%s", ch.Line, ch.Value.Repr())
		}
	}
	return w.String(), nil
}

type groupsID int64

func makeGroupsID(name string) (groupsID, error) {
	if len(name) <= 2 || len(name) > 12 || name[1] != '/' || !(name[0] == 'g' || name[0] == 'u') {
		return 0, fmt.Errorf("forms.InvalidGroupName token=%s: must have g/ or u/ prefix, at most 10 letters", name)
	}
	var r groupsID
	for i, c := range name[2:] {
		if c < 'a' || c > 'z' {
			return 0, fmt.Errorf("forms.NonLetterGroupName token=%s: name must be lowercase letters only", name)
		}
		r += groupsID(c-'a'+1) << (5 * i)
	}
	if name[0] == 'g' {
		return -r, nil
	}
	return r, nil
}

func makeGroupsName(id groupsID) string {
	p := make([]byte, 0, 16)
	if id < 0 {
		p, id = append(p, 'g'), -id
	} else {
		p = append(p, 'u')
	}
	p = append(p, '/')
	for i := 0; i < 10 && id>>(i*5)&31 != 0; i++ {
		p = append(p, byte('a'+id>>(i*5)&31-1))
	}
	return string(p)
}

func (id *groupsID) FromSDINToken(token sdin.Token) error {
	gid, err := makeGroupsID(token.Repr())
	*id = gid
	return err
}

func expandGroups(n *sdin.Node, w *strings.Builder) error {
	groups := map[groupsID][]groupsID{}

	// Parse the groups description.
	nn := sdin.NewNode("", sdin.TFlatStruct())
	for _, ch := range n.Children {
		// Parse and validate the group names.
		gid, err := makeGroupsID(ch.Name)
		if err != nil {
			return fmt.Errorf("forms.ParseGroupName line=%d: %v", ch.Line, err)
		}
		if gid >= 0 {
			return fmt.Errorf("forms.NonGroupName line=%d name=%s: group names must begin with g/, not u/", ch.Line, ch.Name)
		}

		// Initialize the JSON node for the group.
		nch := sdin.NewNode("", sdin.TStruct())
		nn.Children = append(nn.Children, nch)
		nch.Name = fmt.Sprint(gid)

		// Extract the description.
		var desc []string
		for _, comment := range ch.Pre {
			if strings.HasPrefix(comment, "# ") {
				desc = append(desc, comment[2:])
			} else if comment == "#\n" {
				desc = append(desc, "\n")
			}
		}
		if len(desc) != 0 {
			nch.Children = append(nch.Children, sdin.NewNode("description", strings.TrimSpace(strings.Join(desc, ""))))
		}

		// Extract the members.
		members, err := ch.As[[]groupsID]()
		if err != nil {
			return fmt.Errorf("forms.ParseMembers line=%d group=%s: %v", ch.Line, ch.Name, err)
		}
		if _, found := groups[gid]; found {
			return fmt.Errorf("forms.DuplicateGroup line=%d group=%s", ch.Line, ch.Name)
		}
		groups[gid] = members
	}

	// Expand the group members.
	var expand func(gid groupsID) error
	var expanding []groupsID
	expand = func(gid groupsID) error {
		for i, egid := range expanding {
			if egid == gid {
				var names []string
				for j := i; j < len(expanding); j++ {
					names = append(names, makeGroupsName(expanding[j]))
				}
				names = append(names, makeGroupsName(gid))
				return fmt.Errorf("forms.GroupsCycle cycle=%s", names)
			}
		}
		expanding = append(expanding, gid)
		expanded, ok := groups[gid]
		if !ok {
			return fmt.Errorf("forms.NonexistentGroup group=%s", makeGroupsName(gid))
		}
		for _, id := range expanded {
			if id >= 0 {
				continue
			}
			if err := expand(id); err != nil {
				return err
			}
			for _, otherID := range groups[id] {
				expanded = append(expanded, otherID)
			}
		}
		expanded = slices.DeleteFunc(expanded, func(id groupsID) bool { return id < 0 })
		slices.SortFunc(expanded, func(x, y groupsID) int {
			x, y = max(x, -x), max(y, -y)
			for i := range 10 {
				if xx, yy := x>>(5*i)&31, y>>(5*i)&31; xx != yy {
					return int(xx - yy)
				}
			}
			return 0
		})
		groups[gid] = slices.Compact(expanded)
		expanding = expanding[:len(expanding)-1]
		return nil
	}
	for _, gid := range slices.Sorted(maps.Keys(groups)) {
		if err := expand(gid); err != nil {
			return err
		}
	}

	// Add the expanded members to the new JSON tree.
	for _, nch := range nn.Children {
		gid, _ := strconv.ParseInt(nch.Name, 10, 64)
		members := groups[groupsID(gid)]
		mn := sdin.NewNode("members", sdin.TList())
		nch.Children = append(nch.Children, mn)
		for _, m := range members {
			mn.Children = append(mn.Children, sdin.NewNode("", int64(m)))
		}
	}
	slices.SortFunc(nn.Children, func(a, b *sdin.Node) int {
		x, _ := strconv.ParseInt(a.Name, 10, 64)
		y, _ := strconv.ParseInt(b.Name, 10, 64)
		x, y = max(x, -x), max(y, -y)
		for i := range 10 {
			if xx, yy := x>>(5*i)&31, y>>(5*i)&31; xx != yy {
				return int(xx - yy)
			}
		}
		return 0
	})
	data, err := json.MarshalIndent(nn, "", "\t")
	if err != nil {
		return fmt.Errorf("forms.JSONifyGroups: %v", err)
	}
	w.Write(data)
	return nil
}

func (s *solver) Fmt(p string) (string, error) {
	n, err := sdin.Unmarshal[*sdin.Node](p)
	if err != nil {
		return "", fmt.Errorf("fmt.Parse: %v", err)
	}
	errs := sdin.Sanitizer{}.Run(n)
	if errs != nil {
		w := &strings.Builder{}
		w.WriteString("ERROR\n")
		for _, err := range errs {
			w.WriteString(err.Error())
			w.WriteByte('\n')
		}
		return w.String(), nil
	}
	return n.Format(), nil
}

func removeComments(s string) string {
	w := &strings.Builder{}
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
			w.WriteString(line)
		}
	}
	return w.String()
}

func validateIdentifier(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("schema.EmptyIdentifier")
	}
	firstchar, _ := utf8.DecodeRuneInString(s)
	if !unicode.IsUpper(firstchar) {
		return fmt.Errorf("schema.NonUppercaseName firstchar=%q name=%q: names must start with an uppercase letter", firstchar, s)
	}
	for _, ch := range s {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) {
			return fmt.Errorf("schema.NonAlphaNumericName name=%q: names must consists of alphanumeric characters, symbols like underscore cannot be in it", s)
		}
	}
	return nil
}

func validateProperties(n *sdin.Node, properties []*sdin.Node) error {
	for _, property := range properties {
		switch property.Value.Repr() {
		case "checks.MatchToken(":
			if len(property.Children) != 1 {
				return fmt.Errorf("schema.InvalidMatchTokenUsage line=%d: checks.MatchToken needs exactly 1 string argument", property.Line)
			}
			re, err := property.Children[0].As[string]()
			if err != nil {
				return fmt.Errorf("schema.ParseMatchTokenRegex line=%d: %v", property.Line, err)
			}
			ok, err := regexp.MatchString("^("+re+")$", n.Value.Repr())
			if err != nil {
				return fmt.Errorf("schema.CompileMatchTokenRegex line=%d: %v", property.Line, err)
			}
			if !ok {
				return fmt.Errorf("schema.ValidateMatchToken line=%d regexline=%d: MatchToken regex doesn't match", n.Line, property.Line)
			}
		case "checks.MatchString(":
			if len(property.Children) != 1 {
				return fmt.Errorf("schema.InvalidMatchStringUsage line=%d: checks.MatchString needs exactly 1 string argument", property.Line)
			}
			re, err := property.Children[0].As[string]()
			if err != nil {
				return fmt.Errorf("schema.ParseMatchStringRegex line=%d: %v", property.Line, err)
			}
			v, err := n.As[string]()
			if err != nil {
				return fmt.Errorf("schema.ParseNodeStringForMatchString line=%d: %v", n.Line, err)
			}
			ok, err := regexp.MatchString("^("+re+")$", v)
			if err != nil {
				return fmt.Errorf("schema.CompileMatchStringRegex line=%d: %v", property.Line, err)
			}
			if !ok {
				return fmt.Errorf("schema.ValidateMatchString line=%d regexline=%d: MatchString regex doesn't match", n.Line, property.Line)
			}
		case "checks.IntRange(":
			if len(property.Children) != 2 {
				return fmt.Errorf("schema.InvalidIntRangeUsage line=%d: checks.IntRange needs exactly 2 int arguments", property.Line)
			}
			lo, err := property.Children[0].As[int64]()
			if err != nil {
				return fmt.Errorf("schema.ParseIntRangeMin line=%d: %v", property.Line, err)
			}
			hi, err := property.Children[1].As[int64]()
			if err != nil {
				return fmt.Errorf("schema.ParseIntRangeMax line=%d: %v", property.Line, err)
			}
			v, err := n.As[int64]()
			if err != nil {
				return fmt.Errorf("schema.ParseIntForIntRange line=%d: %v", n.Line, err)
			}
			if v < lo || hi < v {
				return fmt.Errorf("schema.ValueOutOfRange line=%d checkline=%d value=%d min=%d max=%d", n.Line, property.Line, v, lo, hi)
			}
		case "checks.Required(":
			// Ignore, this is handled elsewhere.
		default:
			return fmt.Errorf("schema.UnknownProperty line=%d name=%q", property.Line, property.Value.Repr())
		}
	}
	return nil
}

func validateValue(n *sdin.Node, typename string, types map[string]*sdin.Node) error {
	switch typename {
	case "any":
		return nil
	case "bool":
		if _, err := n.Value.As[bool](); err != nil {
			return fmt.Errorf("schema.ParseBool line=%d: %v", n.Line, err)
		}
		return nil
	case "identifier":
		if _, err := n.Value.As[sdin.Identifier](); err != nil {
			return fmt.Errorf("schema.ParseIdentifier line=%d: %v", n.Line, err)
		}
		return nil
	case "int":
		if _, err := n.Value.As[int](); err != nil {
			return fmt.Errorf("schema.ParseInt line=%d: %v", n.Line, err)
		}
		return nil
	case "numeric":
		v := n.Value.Repr()
		if v == "" {
			return fmt.Errorf("schema.ParseEmptyNumeric line=%d: value is empty", n.Line)
		}
		if (v[0] < '0' || '9' < v[0]) && v[0] != '-' && v[0] != '+' {
			return fmt.Errorf("schema.NonNumericToken line=%d token=%q: numeric token must start with a digit or +/-", n.Line, v)
		}
		return nil
	case "string":
		if _, err := n.Value.As[string](); err != nil {
			return fmt.Errorf("schema.ParseString line=%d: %v", n.Line, err)
		}
		return nil
	}

	if elemType, ok := strings.CutPrefix(typename, "list_"); ok {
		if !n.Kind().List() {
			return fmt.Errorf("schema.NonListValue line=%d got=%q typename=%s: this should be a list", n.Line, n.Value.Repr(), typename)
		}
		for i, ch := range n.Children {
			if err := validateValue(ch, elemType, types); err != nil {
				return fmt.Errorf("schema.ValidateListElem line=%d index=%d typename=%s: %v", n.Line, i, typename, err)
			}
		}
		return nil
	}

	if elemType, ok := strings.CutPrefix(typename, "map_"); ok {
		if !n.Kind().Struct() && n.Kind().FormName() != "map" {
			return fmt.Errorf("schema.NonMapValue line=%d got=%q typename=%s: this should be a map", n.Line, n.Value.Repr(), typename)
		}
		keyType, valueType, ok := strings.Cut(elemType, "_")
		if !ok {
			return fmt.Errorf("schema.InvalidMapType line=%d typename=%s: this shouldn't happen because types should be pre-validated", n.Line, typename)
		}
		if n.Kind().FormName() == "map" {
			if len(n.Children)%2 != 0 {
				return fmt.Errorf("schema.InvalidMapChildren line=%d count=%d: there should be an even number of children", n.Line, len(n.Children))
			}
			definedKeys := make(map[string]int, len(n.Children)/2)
			for i := 0; i < len(n.Children); i += 2 {
				normalizedKey := n.Children[i].Value.Repr()
				if n.Children[i].Kind().String() {
					var err error
					normalizedKey, err = n.Children[i].Value.As[string]()
					if err != nil {
						return fmt.Errorf("schema.ParseKeyString line=%d typename=%s: %v", n.Children[i].Line, typename, err)
					}
				}
				if defline, ok := definedKeys[normalizedKey]; ok {
					return fmt.Errorf("schema.DuplicateMapKey line=%d key=%q prevline=%d", n.Children[i].Line, normalizedKey, defline)
				}
				definedKeys[normalizedKey] = n.Children[i].Line
				if err := validateValue(n.Children[i], keyType, types); err != nil {
					return fmt.Errorf("schema.InvalidMapKey line=%d typename=%s: %v", n.Line, typename, err)
				}
				if err := validateValue(n.Children[i+1], valueType, types); err != nil {
					return fmt.Errorf("schema.InvalidMapValue line=%d typename=%s: %v", n.Line, typename, err)
				}
			}
		} else {
			for _, ch := range n.Children {
				kn := &sdin.Node{Value: sdin.MakeTokenFromRepr(ch.Name), Line: ch.Line}
				if err := validateValue(kn, keyType, types); err != nil {
					return fmt.Errorf("schema.InvalidMapKey line=%d typename=%s: %v", n.Line, typename, err)
				}
				if err := validateValue(ch, valueType, types); err != nil {
					return fmt.Errorf("schema.InvalidMapValue line=%d typename=%s: %v", n.Line, typename, err)
				}
			}
		}
		return nil
	}

	tn, found := types[typename]
	if !found {
		return fmt.Errorf("schema.TypeNotFound line=%d typename=%s (this shouldn't happen, this is an internal error)", n.Line, typename)
	}
	basetypename := tn.Value.Repr()
	basetypename = basetypename[:len(basetypename)-1]
	if basetypename == "struct" {
		if !n.Kind().Struct() {
			return fmt.Errorf("schema.NonStructValue line=%d got=%q typename=%s: this should be a struct", n.Line, n.Value.Repr(), typename)
		}
		sn := tn.Children[0]
		for _, ch := range n.Children {
			key := ch.Name
			startRune, _ := utf8.DecodeRuneInString(key)
			if !unicode.IsUpper(startRune) {
				return fmt.Errorf("schema.NonUppercaseIdentifierField line=%d gotField=%q typename=%s: field names must be identifiers and start with uppercase", ch.Line, key, typename)
			}
			sch := sn.Get(key)
			if sch == nil {
				return fmt.Errorf("schema.FieldTypeNotFound line=%d fieldname=%s typename=%s: this type doesn't define this field", ch.Line, key, typename)
			}
			chtypename := sch.Value.Repr()
			chtypename = chtypename[:len(chtypename)-1]
			if err := validateProperties(ch, sch.Children); err != nil {
				return fmt.Errorf("schema.ValidateFieldProperties line=%d fieldname=%s.%s: %v", n.Line, typename, key, err)
			}
			if err := validateValue(ch, chtypename, types); err != nil {
				return fmt.Errorf("schema.ValidateStructField line=%d fieldname=%s typename=%s: %v", n.Line, key, typename, err)
			}
		}
		for _, tch := range sn.Children {
			for _, p := range tch.Children {
				if p.Value.Repr() == "checks.Required(" {
					if n.Get(tch.Name) == nil {
						return fmt.Errorf("schema.RequiredFieldNotFound line=%d checkline=%d fieldname=%s.%s", n.Line, p.Line, typename, tch.Name)
					}
				}
			}
		}
		return nil
	}

	if err := validateProperties(n, tn.Children); err != nil {
		return fmt.Errorf("schema.ValidateValueProperties line=%d typename=%s: %v", n.Line, typename, err)
	}
	if err := validateValue(n, basetypename, types); err != nil {
		return fmt.Errorf("schema.ValidateValue line=%d typename=%s: %v", n.Line, typename, err)
	}
	return nil
}

func validateTypename(types map[string]*sdin.Node, typename string) error {
	for {
		if strings.HasPrefix(typename, "list_") {
			typename = typename[5:]
			continue
		}
		if strings.HasPrefix(typename, "map_") {
			typename = typename[4:]
			key, value, ok := strings.Cut(typename, "_")
			if !ok {
				return fmt.Errorf("schema.InvalidMapValueType typename=%s: map has the syntax map_keytype_valuetype", typename)
			}
			kt := key
			seen := map[string]bool{}
			for {
				startletter, _ := utf8.DecodeRuneInString(kt)
				if !unicode.IsUpper(startletter) {
					break
				}
				if seen[kt] {
					return fmt.Errorf("schema.RecursiveMapKey keytype=%s", kt)
				}
				seen[kt] = true
				tn, found := types[kt]
				if !found {
					return fmt.Errorf("schema.MapKeyTypeNotFound keytype=%s", kt)
				}
				kt = strings.TrimSuffix(tn.Value.Repr(), "(")
			}
			if kt == "struct" || strings.HasPrefix(kt, "list") || strings.HasPrefix(kt, "map") {
				return fmt.Errorf("schema.MapKeyTypeNotPrimitive keytype=%s: map key cannot be struct, list, nor map", kt)
			}
			if _, found := types[kt]; !found {
				return fmt.Errorf("schema.MapKeyBadBuiltin keytype=%s: this builtin type doesn't exist", kt)
			}
			typename = value
			continue
		}
		if _, found := types[typename]; !found {
			return fmt.Errorf("schema.TypeNotFound type=%s", typename)
		}
		return nil
	}
}

func findCycle(types map[string]*sdin.Node, typename string, visiting map[string]bool) []string {
	if visiting[typename] {
		return []string{typename}
	}
	visiting[typename] = true
	defer func() { delete(visiting, typename) }()

	t := typename
	for {
		if strings.HasPrefix(t, "list_") {
			t = t[5:]
		} else if strings.HasPrefix(t, "map_") {
			_, t, _ = strings.Cut(t[4:], "_")
		} else {
			break
		}
	}
	if t != typename {
		if r := findCycle(types, t, visiting); r != nil {
			return append(r, typename)
		}
		return nil
	}
	firstLetter, _ := utf8.DecodeRuneInString(t)
	if !unicode.IsUpper(firstLetter) {
		return nil
	}
	t = types[t].Value.Repr()
	t = t[:len(t)-1]
	if r := findCycle(types, t, visiting); r != nil {
		return append(r, typename)
	}
	return nil
}

type schemaMeta struct {
	PropertyPackages []sdin.Identifier
}

func (s *solver) Schema(p string) (string, error) {
	schemaNode, err := sdin.Unmarshal[*sdin.Node](p)
	if err != nil {
		return "", fmt.Errorf("schema.Parse: %v", err)
	}

	types := map[string]*sdin.Node{}
	for _, t := range []string{"any", "bool", "identifier", "int", "numeric", "string"} {
		types[t] = nil
	}
	consts := map[string]*sdin.Node{}
	packages := map[string]bool{}

	// Collect the top level fields.
	for _, n := range schemaNode.Children {
		key, value := n.Name, n.Value.Repr()
		if key == "meta" {
			m, err := n.As[schemaMeta]()
			if err != nil {
				return "", fmt.Errorf("schema.ParseMeta: %v", err)
			}
			for _, p := range m.PropertyPackages {
				packages[string(p)] = true
			}
			continue
		}
		if err := validateIdentifier(key); err != nil {
			return "", fmt.Errorf("schema.ValidateName line=%d: %v", n.Line, err)
		}
		if value[len(value)-1] != '(' {
			return "", fmt.Errorf("schema.NonFormDefinition line=%d name=%s found=%q: schema definitions must be forms", n.Line, key, value)
		}
		switch value {
		case "const(":
			if len(n.Children) != 2 {
				return "", fmt.Errorf("schema.InvalidConstArgCount line=%d name=%s argCount=%d: const() takes exactly 2 arguments", n.Line, key, len(n.Children))
			}
			if _, err := n.Children[0].As[string](); err == nil {
				return "", fmt.Errorf("schema.NonBareConstType line=%d name=%q: const's type argument must be a bare token, not a string", n.Line, key)
			}
			consts[key] = n
		case "struct(":
			types[key] = n
			if len(n.Children) != 1 || !n.Children[0].Kind().Struct() {
				return "", fmt.Errorf("schema.InvalidStructArg line=%d typename=%s: struct() needs exactly one {} arg", n.Line, n.Name)
			}
			for _, ch := range n.Children[0].Children {
				chkey, chvalue := ch.Name, ch.Value.Repr()
				if err := validateIdentifier(chkey); err != nil {
					return "", fmt.Errorf("schema.ValidateStructFieldName line=%d: %v", ch.Line, err)
				}
				if chvalue[len(chvalue)-1] != '(' {
					return "", fmt.Errorf("schema.NonFormStructFieldDefinition line=%d name=%s.%s found=%q: struct field definitions must be forms", ch.Line, key, chkey, chvalue)
				}
			}
		default:
			types[key] = n
		}
	}

	// Check that all type references exist.
	for _, n := range schemaNode.Children {
		if n.Name == "meta" {
			continue
		}
		value := n.Value.Repr()
		switch value {
		case "const(":
			typename := n.Children[0].Value.Repr()
			if err := validateTypename(types, typename); err != nil {
				return "", fmt.Errorf("schema.ValidateConstType line=%d typename=%s: %v", n.Line, typename, err)
			}
		case "struct(":
			for _, ch := range n.Children[0].Children {
				typename := ch.Value.Repr()
				typename = typename[:len(typename)-1]
				if err := validateTypename(types, typename); err != nil {
					return "", fmt.Errorf("schema.ValidateStructFieldType line=%d field=%s.%s: %v", ch.Line, n.Name, ch.Name, err)
				}
				for _, property := range ch.Children {
					if !strings.HasSuffix(property.Value.Repr(), "(") {
						return "", fmt.Errorf("schema.NonFormProperty line=%d field=%s.%s property=%q", property.Line, n.Name, ch.Name, property.Value.Repr())
					}
					pkg, _, _ := strings.Cut(property.Value.Repr(), ".")
					if !packages[pkg] {
						return "", fmt.Errorf("schema.PackageNotFound line=%d package=%q field=%s.%s property=%q", property.Line, pkg, n.Name, ch.Name, property.Value.Repr())
					}
				}
			}
		default:
			typename := n.Value.Repr()
			typename = typename[:len(typename)-1]
			if err := validateTypename(types, typename); err != nil {
				return "", fmt.Errorf("schema.ValidateType line=%d typename=%s: %v", n.Line, typename, err)
			}
			for _, property := range n.Children {
				if !strings.HasSuffix(property.Value.Repr(), "(") {
					return "", fmt.Errorf("schema.NonFormProperty line=%d type=%s property=%q", property.Line, n.Name, property.Value.Repr())
				}
				pkg, _, _ := strings.Cut(property.Value.Repr(), ".")
				if !packages[pkg] {
					return "", fmt.Errorf("schema.PackageNotFound line=%d package=%q type=%s property=%q", property.Line, pkg, n.Name, property.Value.Repr())
				}
			}
		}
	}

	// Check that no types are recursive.
	visiting := map[string]bool{}
	for _, n := range schemaNode.Children {
		if n.Name == "meta" || n.Value.Repr() == "const(" {
			continue
		}
		if r := findCycle(types, n.Name, visiting); r != nil {
			slices.Reverse(r)
			return "", fmt.Errorf("schema.RecursiveType types=%v", r)
		}
	}

	// Check proto.ID conflicts.
	for _, n := range schemaNode.Children {
		if n.Value.Repr() != "struct(" {
			continue
		}
		ids := make(map[int]string, len(n.Children[0].Children))
		for _, ch := range n.Children[0].Children {
			for _, p := range ch.Children {
				if p.Value.Repr() != "proto.ID(" {
					continue
				}
				if len(p.Children) != 1 {
					return "", fmt.Errorf("schema.InvalidProtoIDArgs line=%d fieldname=%s.%s: proto.ID() takes exactly one integer", p.Line, n.Name, ch.Name)
				}
				id, err := p.Children[0].As[int]()
				if err != nil {
					return "", fmt.Errorf("schema.InvalidProtoIDType line=%d fieldname=%s.%s: proto.ID() takes exactly one integer", p.Line, n.Name, ch.Name)
				}
				f, ok := ids[id]
				if ok {
					return "", fmt.Errorf("schema.DuplicatedProtoID line=%d fieldname=%s.%s prevfield=%s.%s: proto.ID() values must be unique for each field", p.Line, n.Name, ch.Name, n.Name, f)
				}
				ids[id] = ch.Name
			}
		}
		for _, a := range n.Directives() {
			if a.Kind().FormName() != "proto.NextID" {
				continue
			}
			if len(a.Children) != 1 {
				return "", fmt.Errorf("schema.InvalidProtoNextIDArgs line=%d: proto.NextID must have exactly one int argument", a.Line)
			}
			id, err := a.Children[0].As[int]()
			if err != nil {
				return "", fmt.Errorf("schema.ParseProtoIDInt: %v", err)
			}
			if field, found := ids[id]; found {
				return "", fmt.Errorf("schema.AlreadyUsedNextID line=%d id=%d field=%s.%s: proto.NextID should point to the next unused ID", n.Line, id, n.Name, field)
			}
		}
	}

	// Check that the const values validate.
	for _, n := range schemaNode.Children {
		if n.Value.Repr() != "const(" {
			continue
		}
		if err := validateValue(n.Children[1], n.Children[0].Value.Repr(), types); err != nil {
			return "", fmt.Errorf("schema.ValidateConstValue line=%d: %v", n.Line, err)
		}
	}

	return "OK\n", nil
}

// EncodeTestcases formats the given testcase archive into the simplified `task TestCase data=...` format.
func EncodeTestcases(testcases []textar.File) []byte {
	var d []byte
	for _, f := range testcases {
		task, tc, _ := strings.Cut(f.Name, ".")
		d = fmt.Appendf(d, "%s %s data=", task, tc)
		d = hex.AppendEncode(d, f.Data)
		d = append(d, '\n')
	}
	return d
}

// Solve solves the input which is in the format defined by runner.md.
func Solve(testcases []byte) (answer, notes []byte) {
	outw, errw, logw := &bytes.Buffer{}, &bytes.Buffer{}, &bytes.Buffer{}

	defaultLogger, defaultFlags := log.Writer(), log.Flags()
	defer func() { log.SetOutput(defaultLogger); log.SetFlags(defaultFlags) }()
	log.SetOutput(logw)
	log.SetFlags(log.Lshortfile)

	s := &solver{}
	solvertype := reflect.ValueOf(s).Type()
	for tc := range bytes.Lines(testcases) {
		var task, name, hexdata string
		if _, err := fmt.Fscanf(bytes.NewBuffer(tc), "%s %s %s", &task, &name, &hexdata); err != nil {
			return nil, fmt.Appendf(nil, "solver.ParseTestcaseLine: %v", err)
		}
		data, err := hex.DecodeString(strings.TrimPrefix(hexdata, "data="))
		if err != nil {
			return nil, fmt.Appendf(nil, "solver.DecodeTestcaseData testcase=%s.%s: %v", task, name, err)
		}
		outw.WriteString("==NextAnswer==\n")
		errw.WriteString("==NextNotes==\n")
		method, ok := solvertype.MethodByName(strings.ToUpper(task[:1]) + task[1:])
		if !ok {
			errw.WriteString("Error: solver.UnimplementedMethod\n")
			continue
		}
		type wantmethod = func(*solver, string) (string, error)
		methodfunc, ok := method.Func.Interface().(wantmethod)
		if !ok {
			fmt.Fprintf(errw, "Error: solver.BadMethodType task=%s got=%q want=%q\n", task, method.Func.Type(), reflect.TypeFor[wantmethod]())
			continue
		}
		var output string
		func() {
			defer func() {
				if r := recover(); r != nil {
					errw.Write(logw.Bytes())
					logw.Reset()
					fmt.Fprintln(errw, "Runtime panic:")
					fmt.Fprintln(errw, r)
					fmt.Fprintln(errw)
					errw.Write(debug.Stack())
				}
			}()
			output, err = methodfunc(s, string(data))
		}()
		outw.WriteString(output)
		errw.Write(logw.Bytes())
		logw.Reset()
		if err != nil {
			fmt.Fprintf(outw, "ERROR\n%s\n", err)
		}
	}
	return outw.Bytes(), errw.Bytes()
}
