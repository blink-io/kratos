package http

import (
	"net/http"
	"reflect"
	"testing"
)

func TestEmptyCallOptions(t *testing.T) {
	e := EmptyCallOption{}
	if e.Before(&CallInfo{}) != nil {
		t.Error("EmptyCallOption should be ignored")
	}
	e.After(&CallInfo{}, &CsAttempt{})
}

func TestContentType(t *testing.T) {
	if !reflect.DeepEqual(ContentType("aaa").(ContentTypeCallOption).ContentType, "aaa") {
		t.Errorf("want: %v,got: %v", "aaa", ContentType("aaa").(ContentTypeCallOption).ContentType)
	}
}

func TestContentTypeCallOption_before(t *testing.T) {
	c := &CallInfo{}
	err := ContentType("aaa").Before(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual("aaa", c.ContentType) {
		t.Errorf("want: %v, got: %v", "aaa", c.ContentType)
	}
}

func TestAccept(t *testing.T) {
	if !reflect.DeepEqual(Accept("aaa").(AcceptCallOption).ContentType, "aaa") {
		t.Errorf("want: %v,got: %v", "aaa", Accept("aaa").(AcceptCallOption).ContentType)
	}
}

func TestAcceptCallOption_before(t *testing.T) {
	c := &CallInfo{}
	err := Accept("aaa").Before(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual("aaa", c.Accept) {
		t.Errorf("want: %v, got: %v", "aaa", c.Accept)
	}
}

func TestDefaultCallInfo(t *testing.T) {
	path := "hi"
	rv := DefaultCallInfo(path)
	if !reflect.DeepEqual(path, rv.PathTemplate) {
		t.Errorf("expect %v, got %v", path, rv.PathTemplate)
	}
	if !reflect.DeepEqual(path, rv.Operation) {
		t.Errorf("expect %v, got %v", path, rv.Operation)
	}
	if !reflect.DeepEqual("application/json", rv.ContentType) {
		t.Errorf("expect %v, got %v", "application/json", rv.ContentType)
	}
}

func TestOperation(t *testing.T) {
	if !reflect.DeepEqual("aaa", Operation("aaa").(OperationCallOption).Operation) {
		t.Errorf("want: %v,got: %v", "aaa", Operation("aaa").(OperationCallOption).Operation)
	}
}

func TestOperationCallOption_before(t *testing.T) {
	c := &CallInfo{}
	err := Operation("aaa").Before(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual("aaa", c.Operation) {
		t.Errorf("want: %v, got: %v", "aaa", c.Operation)
	}
}

func TestPathTemplate(t *testing.T) {
	if !reflect.DeepEqual("aaa", PathTemplate("aaa").(PathTemplateCallOption).Pattern) {
		t.Errorf("want: %v,got: %v", "aaa", PathTemplate("aaa").(PathTemplateCallOption).Pattern)
	}
}

func TestPathTemplateCallOption_before(t *testing.T) {
	c := &CallInfo{}
	err := PathTemplate("aaa").Before(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual("aaa", c.PathTemplate) {
		t.Errorf("want: %v, got: %v", "aaa", c.PathTemplate)
	}
}

func TestHeader(t *testing.T) {
	h := http.Header{"A": []string{"123"}}
	if !reflect.DeepEqual(Header(&h).(HeaderCallOption).Header.Get("A"), "123") {
		t.Errorf("want: %v,got: %v", "123", Header(&h).(HeaderCallOption).Header.Get("A"))
	}
}

func TestHeaderCallOption_before(t *testing.T) {
	h := http.Header{"A": []string{"123"}}
	c := &CallInfo{}
	o := Header(&h)
	err := o.Before(c)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(&h, c.HeaderCarrier) {
		t.Errorf("want: %v,got: %v", &h, o.(HeaderCallOption).Header)
	}
}

func TestHeaderCallOption_after(t *testing.T) {
	h := http.Header{"A": []string{"123"}}
	c := &CallInfo{}
	cs := &CsAttempt{Res: &http.Response{Header: h}}
	o := Header(&h)
	o.After(c, cs)
	if !reflect.DeepEqual(&h, o.(HeaderCallOption).Header) {
		t.Errorf("want: %v,got: %v", &h, o.(HeaderCallOption).Header)
	}
}
