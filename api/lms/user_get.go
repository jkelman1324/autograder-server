package lms

import (
    "github.com/eriq-augustine/autograder/api/core"
    "github.com/eriq-augustine/autograder/lms"
)

type UserGetRequest struct {
    core.APIRequestCourseUserContext
    core.MinRoleGrader

    TargetUser core.TargetUser `json:"target-email"`
}

type UserGetResponse struct {
    FoundAGUser bool `json:"found-autograder-user"`
    FoundLMSUser bool `json:"found-lms-user"`
    User *core.UserInfo `json:"user"`
}

func HandleUserGet(request *UserGetRequest) (*UserGetResponse, *core.APIError) {
    if (request.Course.GetLMSAdapter() == nil) {
        return nil, core.NewBadRequestError("-501", &request.APIRequest, "Course is not linked to an LMS.").
                Add("course", request.Course.GetID());
    }

    response := UserGetResponse{};

    if (!request.TargetUser.Found) {
        return &response, nil;
    }

    response.FoundAGUser = true;
    response.User = core.NewUserInfo(request.TargetUser.User);

    lmsUser, err := lms.FetchUser(request.Course, string(request.TargetUser.Email));
    if (err != nil) {
        return nil, core.NewInternalError("-502", &request.APIRequestCourseUserContext,
                "Failed to fetch LMS user.").Err(err).Add("email", string(request.TargetUser.Email));
    }

    if (lmsUser == nil) {
        return &response, nil;
    }

    response.FoundLMSUser = true;
    response.User.Name = lmsUser.Name;
    response.User.LMSID = lmsUser.ID;

    return &response, nil;
}
