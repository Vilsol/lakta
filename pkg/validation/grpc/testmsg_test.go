package grpc_test

import (
	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// userMD is a runtime-compiled proto message descriptor
// `User{ email string [(validate).string.email] }`, built with no protoc
// dependency. It is the grpc-side analogue of the fiber DTO with `validate:"email"`.
var userMD = buildUserDescriptor() //nolint:gochecknoglobals // one compiled descriptor shared across the package_test suite

func buildUserDescriptor() protoreflect.MessageDescriptor { //nolint:ireturn // protoreflect exposes descriptors only as interfaces
	opts := &descriptorpb.FieldOptions{}
	proto.SetExtension(opts, validate.E_Field, &validate.FieldRules{
		Type: &validate.FieldRules_String_{
			String_: &validate.StringRules{
				WellKnown: &validate.StringRules_Email{Email: true},
			},
		},
	})

	fdp := &descriptorpb.FileDescriptorProto{
		Name:       new("lakta/validation/testmsg.proto"),
		Syntax:     new("proto3"),
		Package:    new("lakta.validation.testmsg"),
		Dependency: []string{"buf/validate/validate.proto"},
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: new("User"),
			Field: []*descriptorpb.FieldDescriptorProto{{
				Name:     new("email"),
				Number:   proto.Int32(1),
				Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
				JsonName: new("email"),
				Options:  opts,
			}},
		}},
	}

	fd, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		panic(err)
	}
	return fd.Messages().ByName("User")
}

// newUser builds a User message with the given email value.
func newUser(email string) *dynamicpb.Message {
	m := dynamicpb.NewMessage(userMD)
	m.Set(userMD.Fields().ByName("email"), protoreflect.ValueOfString(email))
	return m
}
