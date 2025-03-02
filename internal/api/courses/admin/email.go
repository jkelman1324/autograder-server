package admin

import (
	"errors"
	"fmt"

	"github.com/edulinq/autograder/internal/api/core"
	"github.com/edulinq/autograder/internal/db"
	"github.com/edulinq/autograder/internal/email"
)

type EmailRequest struct {
	core.APIRequestCourseUserContext
	core.MinCourseRoleGrader

	email.Message

	DryRun bool `json:"dry-run"`
}

type EmailResponse struct {
	To  []string `json:"to"`
	CC  []string `json:"cc"`
	BCC []string `json:"bcc"`
}

// Send an email to course users.
func HandleEmail(request *EmailRequest) (*EmailResponse, *core.APIError) {
	response := EmailResponse{}
	var err error

	if request.Subject == "" {
		return nil, core.NewBadRequestError("-627", &request.APIRequest, "No email subject provided.")
	}

	var errs error

	request.To, err = db.ResolveCourseUsers(request.Course, request.To)
	if err != nil {
		err = fmt.Errorf("Failed to resolve 'to' email addresses.")
		errs = errors.Join(errs, err)
	}

	request.CC, err = db.ResolveCourseUsers(request.Course, request.CC)
	if err != nil {
		err = fmt.Errorf("Failed to resolve 'cc' email addresses.")
		errs = errors.Join(errs, err)
	}

	request.BCC, err = db.ResolveCourseUsers(request.Course, request.BCC)
	if err != nil {
		err = fmt.Errorf("Failed to resolve 'bcc' email addresses.")
		errs = errors.Join(errs, err)
	}

	if errs != nil {
		return nil, core.NewInternalError("-628", &request.APIRequestCourseUserContext, errs.Error())
	}

	if (len(request.To) + len(request.CC) + len(request.BCC)) == 0 {
		return nil, core.NewBadRequestError("-629", &request.APIRequest, "No email recipients provided.")
	}

	if !request.DryRun {
		err = email.SendFull(request.To, request.CC, request.BCC, request.Subject, request.Body, false)
		if err != nil {
			return nil, core.NewInternalError("-630", &request.APIRequestCourseUserContext, "Failed to send email.")
		}
	}

	response.To = request.To
	response.CC = request.CC
	response.BCC = request.BCC

	return &response, nil
}
