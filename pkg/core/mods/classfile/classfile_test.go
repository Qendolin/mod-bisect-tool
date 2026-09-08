package classfile

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// --- Class file builder for attribute fixtures ---

// classBuilder incrementally allocates constant pool entries so attribute
// payloads can embed Utf8 indexes that are only known at build time.
type classBuilder struct {
	entries   [][]byte
	nextIndex uint16
}

func newClassBuilder(className string) *classBuilder {
	b := &classBuilder{nextIndex: 1}
	b.utf8(className)
	b.entry(cpClass(1)) // this_class -> Utf8 #1
	return b
}

// utf8 allocates (or reuses) a Utf8 constant and returns its pool index.
func (b *classBuilder) utf8(s string) uint16 {
	for i, entry := range b.entries {
		if len(entry) > 2 && entry[0] == cpTagUtf8 && binary.BigEndian.Uint16(entry[1:3]) == uint16(len(s)) && string(entry[3:]) == s {
			return uint16(i + 1)
		}
	}
	idx := b.nextIndex
	b.nextIndex++
	b.entries = append(b.entries, cpUtf8(s))
	return idx
}

// classRef allocates a Utf8 name and a Class constant, returning the Class
// entry's pool index.
func (b *classBuilder) classRef(className string) uint16 {
	nameIdx := b.utf8(className)
	idx := b.nextIndex
	b.nextIndex++
	b.entries = append(b.entries, cpClass(nameIdx))
	return idx
}

func (b *classBuilder) entry(e []byte) uint16 {
	idx := b.nextIndex
	b.nextIndex++
	b.entries = append(b.entries, e)
	return idx
}

// attribute builds an attribute_info structure for the given content.
func (b *classBuilder) attribute(name string, content []byte) []byte {
	var out bytes.Buffer
	writeU2(&out, b.utf8(name))
	out.Write(binary.BigEndian.AppendUint32(nil, uint32(len(content))))
	out.Write(content)
	return out.Bytes()
}

// build assembles the class file: the allocated constant pool, this/super
// class, empty interfaces/fields/methods and the given class attributes.
func (b *classBuilder) build(attrs ...[]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xCA, 0xFE, 0xBA, 0xBE})
	writeU2(&buf, 0)  // minor
	writeU2(&buf, 52) // major
	writeU2(&buf, uint16(len(b.entries)+1))
	for _, entry := range b.entries {
		buf.Write(entry)
	}
	writeU2(&buf, 0x0021) // access_flags
	writeU2(&buf, 2)      // this_class (always entry #2)
	writeU2(&buf, 0)      // super_class
	writeU2(&buf, 0)      // interfaces_count
	writeU2(&buf, 0)      // fields_count
	writeU2(&buf, 0)      // methods_count
	writeU2(&buf, uint16(len(attrs)))
	for _, attr := range attrs {
		buf.Write(attr)
	}
	return buf.Bytes()
}

// annotationWithClassValue builds an annotation structure with a single 'c'
// element value.
func (b *classBuilder) annotationWithClassValue(annTypeDesc, elemName, classDesc string) []byte {
	var out bytes.Buffer
	writeU2(&out, b.utf8(annTypeDesc))
	writeU2(&out, 1) // one element value pair
	writeU2(&out, b.utf8(elemName))
	out.WriteByte('c')
	writeU2(&out, b.utf8(classDesc))
	return out.Bytes()
}

// --- Binary class file builders ---

func writeU2(buf *bytes.Buffer, v uint16) {
	buf.Write(binary.BigEndian.AppendUint16(nil, v))
}

// classFileBytes assembles a minimal but structurally valid class file from
// raw constant pool entry payloads. cpCount is the constant_pool_count to
// declare; normally len(cpEntries)+1, +2 per two-slot constant.
func classFileBytes(thisClassIndex, cpCount uint16, cpEntries ...[]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xCA, 0xFE, 0xBA, 0xBE}) // magic
	writeU2(&buf, 0)                          // minor_version
	writeU2(&buf, 52)                         // major_version
	writeU2(&buf, cpCount)                    // constant_pool_count
	for _, entry := range cpEntries {
		buf.Write(entry)
	}
	writeU2(&buf, 0x0021)         // access_flags (public super)
	writeU2(&buf, thisClassIndex) // this_class
	writeU2(&buf, 0)              // super_class
	writeU2(&buf, 0)              // interfaces_count
	writeU2(&buf, 0)              // fields_count
	writeU2(&buf, 0)              // methods_count
	writeU2(&buf, 0)              // attributes_count
	return buf.Bytes()
}

func cpUtf8(s string) []byte {
	out := binary.BigEndian.AppendUint16([]byte{cpTagUtf8}, uint16(len(s)))
	return append(out, s...)
}

func cpClass(nameIndex uint16) []byte {
	return binary.BigEndian.AppendUint16([]byte{cpTagClass}, nameIndex)
}

func cpNameAndType(nameIndex, descriptorIndex uint16) []byte {
	out := []byte{cpTagNameAndType}
	out = binary.BigEndian.AppendUint16(out, nameIndex)
	return binary.BigEndian.AppendUint16(out, descriptorIndex)
}

func cpLong() []byte {
	return append([]byte{cpTagLong}, make([]byte, 8)...)
}

func cpModule(nameIndex uint16) []byte {
	return binary.BigEndian.AppendUint16([]byte{cpTagModule}, nameIndex)
}

func cpPackage(nameIndex uint16) []byte {
	return binary.BigEndian.AppendUint16([]byte{cpTagPackage}, nameIndex)
}

// cpUnknown produces a constant pool entry with a tag no JVM spec ever defined.
func cpUnknown(tag byte) []byte {
	return []byte{tag}
}

// --- Parser tests ---

func TestParseClassFileMinimal(t *testing.T) {
	// #1 Utf8 "com/example/Foo", #2 Class -> #1.
	data := classFileBytes(2, 3, cpUtf8("com/example/Foo"), cpClass(1))

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}
	if info.Name != "com/example/Foo" {
		t.Errorf("expected declared name %q, got %q", "com/example/Foo", info.Name)
	}
	if len(info.References) != 0 {
		t.Errorf("expected no references, got %v", info.References)
	}
}

func TestParseClassFileExtractsAndDeduplicatesReferences(t *testing.T) {
	// #1 own name, #2/#3 referenced names, #4 -> #2, #5 -> #3, #6 -> #2 (dup), #7 -> #1 (this).
	data := classFileBytes(7, 8,
		cpUtf8("com/example/Foo"),
		cpUtf8("com/example/Other"),
		cpUtf8("java/lang/Object"),
		cpClass(2),
		cpClass(3),
		cpClass(2),
		cpClass(1),
	)

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}
	expected := []string{"com/example/Other", "java/lang/Object"}
	if len(info.References) != len(expected) {
		t.Fatalf("expected references %v, got %v", expected, info.References)
	}
	for _, ref := range expected {
		if !slices.Contains(info.References, ref) {
			t.Errorf("expected reference %q in %v", ref, info.References)
		}
	}
}

func TestParseClassFileExcludesSelfReference(t *testing.T) {
	// InnerClasses attributes reference the class itself via separate pool
	// entries; these must not show up as references.
	data := classFileBytes(2, 4,
		cpUtf8("com/x/Self"),
		cpClass(1), // this_class
		cpClass(1), // separate self reference
	)

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}

	if len(info.References) != 0 {
		t.Errorf("expected self references to be excluded, got %v", info.References)
	}
}

func TestParseClassFileExtractsDescriptorReferences(t *testing.T) {
	// #1 own name, #2 method descriptor, #3 NameAndType using #2, #4 this_class.
	data := classFileBytes(4, 5,
		cpUtf8("com/example/Source"),
		cpUtf8("(Lcom/example/arg/Argument;[Lcom/example/target/Target;)Lcom/example/result/Result;"),
		cpNameAndType(1, 2),
		cpClass(1),
	)

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}
	for _, ref := range []string{
		"com/example/arg/Argument",
		"com/example/target/Target",
		"com/example/result/Result",
	} {
		if !slices.Contains(info.References, ref) {
			t.Errorf("expected descriptor reference %q in %v", ref, info.References)
		}
	}
}

func TestParseClassFileUnknownTagErrors(t *testing.T) {
	// Tag 42 is not defined by any JVM spec; a future tag must fail loudly
	// instead of being silently misparsed.
	data := classFileBytes(2, 4,
		cpUtf8("com/example/Foo"),
		cpClass(1),
		cpUnknown(42),
	)

	_, err := ParseClassFile(data)
	if err == nil {
		t.Fatal("expected an error for an unknown constant pool tag, got nil")
	}
	if !strings.Contains(err.Error(), "42") {
		t.Errorf("expected the error to mention the offending tag, got: %v", err)
	}
}

func TestParseClassFileTwoSlotConstants(t *testing.T) {
	// CONSTANT_Long occupies two pool slots (#2 and #3); the Utf8 that follows
	// is #4. Verifies the two-slot advance.
	data := classFileBytes(6, 7,
		cpUtf8("com/x/HasLong"),
		cpLong(),
		cpUtf8("java/lang/Object"),
		cpClass(4),
		cpClass(1),
	)

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}
	if info.Name != "com/x/HasLong" {
		t.Errorf("expected declared name %q, got %q", "com/x/HasLong", info.Name)
	}
	if !slices.Contains(info.References, "java/lang/Object") {
		t.Errorf("expected java/lang/Object in references after a two-slot constant, got %v", info.References)
	}
}

func TestParseClassFileModuleInfo(t *testing.T) {
	// module-info.class uses CONSTANT_Module/CONSTANT_Package entries.
	data := classFileBytes(4, 5,
		cpUtf8("module-info"),
		cpModule(1),
		cpPackage(1),
		cpClass(1),
	)

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed for module-info: %v", err)
	}
	if info.Name != "module-info" {
		t.Errorf("expected declared name %q, got %q", "module-info", info.Name)
	}
}

func TestParseClassFileTruncatedErrors(t *testing.T) {
	full := classFileBytes(2, 3, cpUtf8("com/example/Foo"), cpClass(1))
	// The parser stops after this_class, so only truncations within the magic,
	// header, constant pool, access flags or this_class can be detected.
	for _, cut := range []int{4, 10, len(full) - 16, len(full) - 12} {
		if _, err := ParseClassFile(full[:cut]); err == nil {
			t.Errorf("expected an error for a class file truncated to %d bytes, got nil", cut)
		}
	}
}

func TestParseClassFileBadMagicErrors(t *testing.T) {
	data := classFileBytes(2, 3, cpUtf8("com/example/Foo"), cpClass(1))
	data[0] = 0xDE
	if _, err := ParseClassFile(data); err == nil {
		t.Fatal("expected an error for a bad magic number, got nil")
	}
}

// --- Tests against real class files compiled with javac (pre-compiled in testdata/classes) ---

func loadTestdataClasses(t *testing.T) map[string]ClassInfo {
	t.Helper()
	testdataDir := filepath.Join("testdata", "classes")
	byName := make(map[string]ClassInfo)
	err := filepath.WalkDir(testdataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".class") {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read testdata class %s: %v", path, err)
			}
			info, err := ParseClassFile(data)
			if err != nil {
				t.Fatalf("ParseClassFile failed for %s: %v", path, err)
			}
			byName[filepath.Base(path)] = info
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk testdata/classes: %v", err)
	}
	return byName
}

func TestParseClassFileCompiledWithJavac(t *testing.T) {
	byName := loadTestdataClasses(t)
	if len(byName) == 0 {
		t.Fatal("no testdata class files found in testdata/classes")
	}

	a, ok := byName["A.class"]
	if !ok {
		t.Fatalf("A.class not found in testdata")
	}
	if a.Name != "com/example/a/A" {
		t.Errorf("expected A.class to declare com/example/a/A, got %q", a.Name)
	}
	if !slices.Contains(a.References, "java/lang/Object") {
		t.Errorf("expected A.class to reference java/lang/Object, got %v", a.References)
	}

	b, ok := byName["B.class"]
	if !ok {
		t.Fatalf("B.class not found in testdata")
	}
	if b.Name != "com/example/b/B" {
		t.Errorf("expected B.class to declare com/example/b/B, got %q", b.Name)
	}
	// Cross-class references via Methodref / InterfaceMethodref.
	if !slices.Contains(b.References, "com/example/a/A") {
		t.Errorf("expected B.class to reference com/example/a/A, got %v", b.References)
	}
	if !slices.Contains(b.References, "java/lang/Runnable") {
		t.Errorf("expected B.class to reference java/lang/Runnable, got %v", b.References)
	}

	// Annotated.class references A only through annotation class values (on
	// the class, a field and a parameter); without annotation parsing the
	// reference would be invisible.
	annotated, ok := byName["Annotated.class"]
	if !ok {
		t.Fatalf("Annotated.class not found in testdata")
	}
	if annotated.Name != "com/example/b/Annotated" {
		t.Errorf("expected Annotated.class to declare com/example/b/Annotated, got %q", annotated.Name)
	}
	if !slices.Contains(annotated.References, "com/example/a/A") {
		t.Errorf("expected Annotated.class to reference com/example/a/A via annotation class values, got %v", annotated.References)
	}
	// The annotation's own type is a runtime-resolved class reference too.
	if !slices.Contains(annotated.References, "com/example/a/RuntimeRef") {
		t.Errorf("expected Annotated.class to reference the annotation type com/example/a/RuntimeRef, got %v", annotated.References)
	}

	// TypeUseAnnotated.class references classes only through type-use
	// annotations in code bodies (instanceof, catch clause, generic type
	// argument) — the target_info paths that previously desynced the parser.
	typeUseAnnotated, ok := byName["TypeUseAnnotated.class"]
	if !ok {
		t.Fatalf("TypeUseAnnotated.class not found in testdata")
	}
	for _, want := range []string{"java/lang/String", "java/lang/RuntimeException", "java/lang/Integer"} {
		if !slices.Contains(typeUseAnnotated.References, want) {
			t.Errorf("expected TypeUseAnnotated.class to reference %s via type-use annotations, got %v", want, typeUseAnnotated.References)
		}
	}

	mi, ok := byName["module-info.class"]
	if !ok {
		t.Fatalf("module-info.class not found in testdata")
	}
	if mi.Name != "module-info" {
		t.Errorf("expected module-info.class to declare module-info, got %q", mi.Name)
	}
}

// --- Annotation attribute fixtures ---

// appendMemberAttributes writes a field_info or method_info skeleton with the
// given attribute table appended to buf.
func appendMemberAttributes(buf *bytes.Buffer, b *classBuilder, attrs ...[]byte) {
	writeU2(buf, 0x0002)                       // access_flags (private)
	writeU2(buf, b.utf8("member"))             // name_index
	writeU2(buf, b.utf8("Ljava/lang/String;")) // descriptor_index
	writeU2(buf, uint16(len(attrs)))
	for _, attr := range attrs {
		buf.Write(attr)
	}
}

// buildWithMembers assembles a class file with one field and one method, each
// carrying an attribute table, plus class attributes.
func (b *classBuilder) buildWithMembers(fieldAttrs, methodAttrs, classAttrs [][]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xCA, 0xFE, 0xBA, 0xBE})
	writeU2(&buf, 0)  // minor
	writeU2(&buf, 52) // major
	writeU2(&buf, uint16(len(b.entries)+1))
	for _, entry := range b.entries {
		buf.Write(entry)
	}
	writeU2(&buf, 0x0021) // access_flags
	writeU2(&buf, 2)      // this_class
	writeU2(&buf, 0)      // super_class
	writeU2(&buf, 0)      // interfaces_count

	writeU2(&buf, 1) // fields_count
	appendMemberAttributes(&buf, b, fieldAttrs...)

	writeU2(&buf, 1) // methods_count
	appendMemberAttributes(&buf, b, methodAttrs...)

	writeU2(&buf, uint16(len(classAttrs)))
	for _, attr := range classAttrs {
		buf.Write(attr)
	}
	return buf.Bytes()
}

func assertContainsRef(t *testing.T, refs []string, want string) {
	t.Helper()
	if !slices.Contains(refs, want) {
		t.Errorf("expected reference %q in %v", want, refs)
	}
}

// TestParseClassFileAnnotationClassValues verifies that class values in
// RuntimeVisibleAnnotations are collected as references, including array
// values and nested annotations. Array descriptors unwrap to the component
// class; primitive arrays yield nothing.
func TestParseClassFileAnnotationClassValues(t *testing.T) {
	b := newClassBuilder("com/example/Foo")

	// Single class value: @Ann(value = Lcom/x/Y;)
	ann1 := b.annotationWithClassValue("Lcom/Ann;", "value", "Lcom/x/Y;")

	// Array of class values: @Arr({Lcom/x/A;, [Lcom/x/B;})
	var arrAnn bytes.Buffer
	writeU2(&arrAnn, b.utf8("Lcom/Arr;"))
	writeU2(&arrAnn, 1)
	writeU2(&arrAnn, b.utf8("value"))
	arrAnn.WriteByte('[')
	writeU2(&arrAnn, 2) // two element values
	arrAnn.WriteByte('c')
	writeU2(&arrAnn, b.utf8("Lcom/x/A;"))
	arrAnn.WriteByte('c')
	writeU2(&arrAnn, b.utf8("[Lcom/x/B;"))
	arrContent := arrAnn.Bytes()

	// Nested annotation: @Outer(inner = @Inner(value = Lcom/x/C;))
	var nestedAnn bytes.Buffer
	writeU2(&nestedAnn, b.utf8("Lcom/Outer;"))
	writeU2(&nestedAnn, 1)
	writeU2(&nestedAnn, b.utf8("inner"))
	nestedAnn.WriteByte('@')
	nestedAnn.Write(b.annotationWithClassValue("Lcom/Inner;", "value", "Lcom/x/C;"))

	// A CONSTANT_Class array reference (Foo[].class in normal code) unwraps too.
	b.classRef("[Lcom/x/Arr;")

	info, err := ParseClassFile(b.build(
		b.attribute("RuntimeVisibleAnnotations", concatAnnotations(ann1, arrContent, nestedAnn.Bytes())),
	))
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}

	assertContainsRef(t, info.References, "com/x/Y")
	assertContainsRef(t, info.References, "com/x/A")
	assertContainsRef(t, info.References, "com/x/B") // unwrapped from [Lcom/x/B;
	assertContainsRef(t, info.References, "com/x/C") // from the nested annotation
	assertContainsRef(t, info.References, "com/x/Arr")
	for _, ref := range info.References {
		if strings.HasPrefix(ref, "[") {
			t.Errorf("expected array descriptors to be unwrapped, got %q in %v", ref, info.References)
		}
	}
}

// concatAnnotations prefixes an annotation list with its element count.
func concatAnnotations(anns ...[]byte) []byte {
	var out bytes.Buffer
	writeU2(&out, uint16(len(anns)))
	for _, ann := range anns {
		out.Write(ann)
	}
	return out.Bytes()
}

// TestParseClassFileMemberAnnotationAttributes verifies that annotation
// attributes on fields and methods (invisible annotations, parameter
// annotations, type annotations with target info, and annotation defaults)
// are walked and their class values collected.
func TestParseClassFileMemberAnnotationAttributes(t *testing.T) {
	b := newClassBuilder("com/example/Foo")

	fieldAnn := b.annotationWithClassValue("Lcom/FAnn;", "value", "Lcom/x/Field;")

	// RuntimeInvisibleParameterAnnotations: one parameter, one annotation.
	var paramAnn bytes.Buffer
	paramAnn.WriteByte(1) // num_parameters
	writeU2(&paramAnn, 1) // annotation count for parameter 0
	paramAnn.Write(b.annotationWithClassValue("Lcom/PAnn;", "value", "Lcom/x/Param;"))

	// RuntimeVisibleTypeAnnotations covering the target_info variants: local
	// variable (0x40), catch (0x42), type parameter bound (0x11), throws
	// (0x17), offset (0x43) and type argument (0x47).
	typeAnnotation := func(targetType byte, targetInfo []byte, classDesc string) []byte {
		var out bytes.Buffer
		out.WriteByte(targetType)
		out.Write(targetInfo)
		out.WriteByte(0) // type_path length
		out.Write(b.annotationWithClassValue("Lcom/TAnn;", "value", classDesc))
		return out.Bytes()
	}
	var typeAnn bytes.Buffer
	writeU2(&typeAnn, 6) // six type annotations
	// Local variable target: table_length=1, one entry (start_pc, length, index).
	typeAnn.Write(typeAnnotation(0x40, []byte{0, 1, 0, 1, 0, 1, 0, 0}, "Lcom/x/LocalVar;"))
	// Catch target: exception_table_index.
	typeAnn.Write(typeAnnotation(0x42, []byte{0, 0}, "Lcom/x/Catch;"))
	// Type parameter bound target: type_parameter_index, bound_index.
	typeAnn.Write(typeAnnotation(0x11, []byte{0, 0}, "Lcom/x/Bound;"))
	// Throws target: throws_type_index.
	typeAnn.Write(typeAnnotation(0x17, []byte{0, 1}, "Lcom/x/Throws;"))
	// Offset target: offset.
	typeAnn.Write(typeAnnotation(0x43, []byte{0, 1}, "Lcom/x/Offset;"))
	// Type argument target: offset, type_argument_index.
	typeAnn.Write(typeAnnotation(0x47, []byte{0, 1, 0}, "Lcom/x/TypeArg;"))

	// AnnotationDefault with a class value.
	var def bytes.Buffer
	def.WriteByte('c')
	writeU2(&def, b.utf8("Lcom/x/Default;"))

	info, err := ParseClassFile(b.buildWithMembers(
		[][]byte{b.attribute("RuntimeInvisibleAnnotations", concatAnnotations(fieldAnn))},
		[][]byte{
			b.attribute("RuntimeInvisibleParameterAnnotations", paramAnn.Bytes()),
			b.attribute("RuntimeVisibleTypeAnnotations", typeAnn.Bytes()),
			b.attribute("AnnotationDefault", def.Bytes()),
		},
		nil,
	))
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}

	for _, want := range []string{"com/x/Field", "com/x/Param", "com/x/LocalVar", "com/x/Catch", "com/x/Bound", "com/x/Throws", "com/x/Offset", "com/x/TypeArg", "com/x/Default"} {
		assertContainsRef(t, info.References, want)
	}
}

// TestParseClassFileUnknownElementValueTagErrors verifies the strictness rule:
// an unknown annotation element value tag must fail loudly.
func TestParseClassFileUnknownElementValueTagErrors(t *testing.T) {
	b := newClassBuilder("com/example/Foo")

	var ann bytes.Buffer
	writeU2(&ann, b.utf8("Lcom/Ann;"))
	writeU2(&ann, 1)
	writeU2(&ann, b.utf8("value"))
	ann.WriteByte('w') // tag no JVM spec ever defined
	writeU2(&ann, b.utf8("Lcom/x/Y;"))

	_, err := ParseClassFile(b.build(
		b.attribute("RuntimeVisibleAnnotations", concatAnnotations(ann.Bytes())),
	))
	if err == nil {
		t.Fatal("expected an error for an unknown element value tag, got nil")
	}
	if !strings.Contains(err.Error(), "unknown annotation element value tag") {
		t.Errorf("expected the error to name the element value tag, got: %v", err)
	}
}

func TestParseClassFileDescriptorWithInternalL(t *testing.T) {
	// Descriptor containing classes with internal uppercase 'L':
	// java/util/List must not produce "ist"
	// com/example/ClassLoader must not produce "oader"
	data := classFileBytes(4, 5,
		cpUtf8("com/example/Source"),
		cpUtf8("(Ljava/util/List;Lcom/example/ClassLoader;)V"),
		cpNameAndType(1, 2),
		cpClass(1),
	)

	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}

	for _, bogus := range []string{"ist", "oader"} {
		if slices.Contains(info.References, bogus) {
			t.Errorf("bogus truncated reference %q extracted from descriptor", bogus)
		}
	}

	for _, want := range []string{"java/util/List", "com/example/ClassLoader"} {
		if !slices.Contains(info.References, want) {
			t.Errorf("expected reference %q, got %v", want, info.References)
		}
	}
}

func TestParseClassFileSignaturesAndGenerics(t *testing.T) {
	b := newClassBuilder("com/example/Test")

	// Class-level Signature attribute: generic superclass & interface
	// Lcom/example/Super<Lcom/example/TypeArg;>;Lcom/example/Interface<[Lcom/example/ArrayArg;>;
	sig := "Lcom/example/Super<Lcom/example/TypeArg;>;Lcom/example/Interface<[Lcom/example/ArrayArg;>;"
	sigAttr := b.attribute("Signature", binary.BigEndian.AppendUint16(nil, b.utf8(sig)))

	data := b.build(sigAttr)
	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}

	expected := []string{
		"com/example/Super",
		"com/example/TypeArg",
		"com/example/Interface",
		"com/example/ArrayArg",
	}
	for _, want := range expected {
		if !slices.Contains(info.References, want) {
			t.Errorf("missing expected reference %q, got %v", want, info.References)
		}
	}
}

func TestParseClassFileMalformedSignatureAttribute(t *testing.T) {
	b := newClassBuilder("com/example/Test")
	// Declared length is 0 (invalid for Signature, which requires length 2)
	badSigAttr := b.attribute("Signature", []byte{})

	data := b.build(badSigAttr)
	_, err := ParseClassFile(data)
	if err == nil {
		t.Fatal("expected error for Signature attribute with length < 2, got nil")
	}
}

func TestParseClassFileAnnotationEnumValue(t *testing.T) {
	b := newClassBuilder("com/example/Foo")

	var ann bytes.Buffer
	writeU2(&ann, b.utf8("Lcom/example/Ann;"))
	writeU2(&ann, 1)
	writeU2(&ann, b.utf8("mode"))
	ann.WriteByte('e')
	writeU2(&ann, b.utf8("Lcom/example/MyEnum;"))
	writeU2(&ann, b.utf8("VALUE"))

	data := b.build(b.attribute("RuntimeVisibleAnnotations", concatAnnotations(ann.Bytes())))
	info, err := ParseClassFile(data)
	if err != nil {
		t.Fatalf("ParseClassFile failed: %v", err)
	}

	assertContainsRef(t, info.References, "com/example/MyEnum")
	assertContainsRef(t, info.References, "com/example/Ann")
}
