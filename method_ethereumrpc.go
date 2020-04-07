package openrpc_go_document

import (
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/davecgh/go-spew/spew"
	goopenrpcT "github.com/gregdhill/go-openrpc/types"
)

func DefaultEthereumServiceProvider(wrapped interface{}) *ServerProviderService {
	return &ServerProviderService{
		ServiceCallbacksFromReceiver: DefaultServiceCallbacksEthereum(wrapped),
		ServiceCallbackToMethod:      DefaultServiceCallbackToMethodEthereum,
		ServiceOpenRPCInfo:           func() goopenrpcT.Info { return goopenrpcT.Info{} },
		ServiceOpenRPCExternalDocs: func() *goopenrpcT.ExternalDocs{
			return &goopenrpcT.ExternalDocs{
				Description: "GPLv3",
				URL:         "https://github.com/ethereum/go-ethereum/blob/COPYING.md",
			}
		},
	}
}

func DefaultEthereumParseOptions() *DocumentProviderParseOpts {
	opts := DefaultParseOptions()
	opts.ContentDescriptorSkipFn = func(isArgs bool, index int, cd *goopenrpcT.ContentDescriptor) bool {
		if isArgs && index == 0 && strings.Contains(cd.Description, "context") {
			return true
		}
		return false
	}
	return opts
}

// DefaultServiceCallbackToMethodEthereum will parse a method to an openrpc method.
// Note that this will collect only the broad strokes:
// - all args, result[0] values => params, result
//
// ContentDescriptors and/or Schema filters must be applied separately.
func DefaultServiceCallbackToMethodEthereum(opts *DocumentProviderParseOpts, name string, cb Callback) (*goopenrpcT.Method, error) {
	pcb, err := newParsedCallback(cb)
	if err != nil {
		if strings.Contains(err.Error(), "autogenerated") {
			return nil, errParseCallbackAutoGenerate
		}
		log.Println("parse ethereumCallback", err)
		return nil, err
	}
	method, err := makeEthereumMethod(opts, name, pcb)
	if err != nil {
		return nil, fmt.Errorf("make method error method=%s cb=%s error=%v", name, spew.Sdump(cb), err)
	}
	return method, nil
}

func makeEthereumMethod(opts *DocumentProviderParseOpts, name string, pcb *parsedCallback) (*goopenrpcT.Method, error) {

	argTypes := pcb.cb.getArgTypes()
	retTyptes := pcb.cb.getRetTypes()

	argASTFields := []*NamedField{}
	if pcb.fdecl.Type != nil &&
		pcb.fdecl.Type.Params != nil &&
		pcb.fdecl.Type.Params.List != nil {
		for _, f := range pcb.fdecl.Type.Params.List {
			argASTFields = append(argASTFields, expandASTField(f)...)
		}
	}

	retASTFields := []*NamedField{}
	if pcb.fdecl.Type != nil &&
		pcb.fdecl.Type.Results != nil &&
		pcb.fdecl.Type.Results.List != nil {
		for _, f := range pcb.fdecl.Type.Results.List {
			retASTFields = append(retASTFields, expandASTField(f)...)
		}
	}

	description := func() string {
		return fmt.Sprintf("`%s`", pcb.cb.Func().Type().String())
	}

	contentDescriptor := func(ty reflect.Type, astNamedField *NamedField) (*goopenrpcT.ContentDescriptor, error) {
		sch := typeToSchema(opts, ty)
		if opts != nil && len(opts.SchemaMutationFns) > 0 {
			for _, mutation := range opts.SchemaMutationFns {
				if err := mutation(&sch); err != nil {
					return nil, err
				}
			}
		}
		return &goopenrpcT.ContentDescriptor{
			Content: goopenrpcT.Content{
				Name:        astNamedField.Name,
				Summary:     astNamedField.Field.Comment.Text(),
				Required:    true,
				Description: fullTypeDescription(ty),
				Schema:      sch,
			},
		}, nil
	}

	params := func(skipFn func(isArgs bool, index int, descriptor *goopenrpcT.ContentDescriptor) bool) ([]*goopenrpcT.ContentDescriptor, error) {
		out := []*goopenrpcT.ContentDescriptor{}
		for i, a := range argTypes {
			cd, err := contentDescriptor(a, argASTFields[i])
			if err != nil {
				return nil, err
			}
			if skipFn != nil && skipFn(true, i, cd) {
				continue
			}
			for _, fn := range opts.ContentDescriptorMutationFns {
				fn(true, i, cd)
			}
			out = append(out, cd)
		}
		return out, nil
	}

	rets := func(skipFn func(isArgs bool, index int, descriptor *goopenrpcT.ContentDescriptor) bool) ([]*goopenrpcT.ContentDescriptor, error) {
		out := []*goopenrpcT.ContentDescriptor{}
		for i, r := range retTyptes {
			cd, err := contentDescriptor(r, retASTFields[i])
			if err != nil {
				return nil, err
			}
			if skipFn != nil && skipFn(false, i, cd) {
				continue
			}
			for _, fn := range opts.ContentDescriptorMutationFns {
				fn(false, i, cd)
			}
			out = append(out, cd)
		}
		if len(out) == 0 {
			out = append(out, nullContentDescriptor)
		}
		return out, nil
	}

	runtimeFile, runtimeLine := pcb.runtimeF.FileLine(pcb.runtimeF.Entry())

	collectedParams, err := params(opts.ContentDescriptorSkipFn)
	if err != nil {
		return nil, err
	}
	collectedResults, err := rets(opts.ContentDescriptorSkipFn)
	if err != nil {
		return nil, err
	}
	res := collectedResults[0] // OpenRPC Document specific

	method := newMethod()
	method.Name = name
	method.Summary = methodSummary(pcb.fdecl)
	method.Description = description()
	method.ExternalDocs = goopenrpcT.ExternalDocs{
		Description: fmt.Sprintf("line=%d", runtimeLine),
		URL:         fmt.Sprintf("file://%s", runtimeFile), // TODO: Provide WORKING external docs links to Github (actually a wrapper/injection to make this configurable).
	}
	method.Params = collectedParams
	method.Result = res
	method.Deprecated = methodDeprecated(pcb.fdecl)
	return method, nil

	//return &goopenrpcT.Method{
	//	Name:        name, // pcb.runtimeF.Name(), // FIXME or give me a comment.
	//	Tags:        nil,
	//	Summary:     methodSummary(pcb.fdecl),
	//	Description: description(),
	//	ExternalDocs: goopenrpcT.ExternalDocs{
	//		Description: fmt.Sprintf("line=%d", runtimeLine),
	//		URL:         fmt.Sprintf("file://%s", runtimeFile), // TODO: Provide WORKING external docs links to Github (actually a wrapper/injection to make this configurable).
	//	},
	//	Params:         collectedParams,
	//	Result:         res,
	//	Deprecated:     methodDeprecated(pcb.fdecl),
	//	Servers:        nil,
	//	Errors:         nil,
	//	Links:          nil,
	//	ParamStructure: "by-position",
	//	Examples:       nil,
	//}, nil
	//
}
