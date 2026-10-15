module SessionTests exposing (suite)

import Api.Session as Api
import Domain.Session as Domain exposing (Role(..))
import Feature.Session as Session
import Http
import Json.Decode as Decode
import String


suite : List ( String, Bool )
suite =
    [ ( "session response decoder handles shared roles", caseSessionDecoder )
    , ( "session decoder rejects incomplete access data", caseRejectMalformedSession )
    , ( "session endpoints are same origin by default", caseSameOriginEndpoints )
    , ( "login drafts clear before sending and on failure", caseLoginDraftClearing )
    , ( "login success emits session changed and exposes permissions", caseLoginSuccess )
    , ( "stale session response is ignored", caseStaleResponse )
    , ( "account creation clears password before sending", caseAccountPasswordClearing )
    , ( "viewer cannot start account administration", caseViewerCannotManage )
    , ( "closing login clears password draft", caseCloseClearsDraft )
    ]


caseSessionDecoder : Bool
caseSessionDecoder =
    case Decode.decodeString Domain.sessionDecoder adminJSON of
        Ok session ->
            session.privateMode
                && session.authenticated
                && session.canRead
                && session.canWrite
                && session.canModerate
                && session.isAdmin
                && session.user
                == Just { id = "12", handle = "owner", role = Administrator }

        Err _ ->
            False


caseRejectMalformedSession : Bool
caseRejectMalformedSession =
    case Decode.decodeString Domain.sessionDecoder """{"private_mode":true,"authenticated":false,"user":null,"can_read":true,"can_write":false}""" of
        Err _ ->
            True

        Ok _ ->
            False


caseSameOriginEndpoints : Bool
caseSameOriginEndpoints =
    Api.endpoint "" [ "api", "auth", "session" ]
        == "/api/auth/session"
        && Api.endpoint "" [ "api", "auth", "users", "2" ]
        == "/api/auth/users/2"
        && Api.endpoint "http://localhost:8080" [ "api", "auth", "session" ]
        == "http://localhost:8080/api/auth/session"


caseLoginDraftClearing : Bool
caseLoginDraftClearing =
    let
        ctx =
            Session.context "" 4

        ( initial, _ ) =
            Session.init ctx

        ( privateAnonymous, _, _ ) =
            Session.update ctx (Session.SessionLoaded 4 1 (Ok anonymousSession)) initial

        ( withHandle, _, _ ) =
            Session.update ctx (Session.HandleChanged "owner") privateAnonymous

        ( withPassword, _, _ ) =
            Session.update ctx (Session.PasswordChanged "private-secret") withHandle

        ( requested, _, _ ) =
            Session.update ctx Session.LoginRequested withPassword

        ( failed, _, _ ) =
            Session.update ctx (Session.LoginCompleted 4 requested.generation (Err (Http.BadStatus 401))) requested
    in
    requested.passwordDraft
        == ""
        && requested.handleDraft
        == ""
        && requested.loginBusy
        && failed.passwordDraft
        == ""
        && failed.handleDraft
        == ""
        && not failed.loginBusy
        && failed.notice
        == Just "Handle or password was not accepted."
        && not (String.contains "private-secret" (Maybe.withDefault "" failed.notice))
        && not (Session.canRead failed)


caseLoginSuccess : Bool
caseLoginSuccess =
    let
        ctx =
            Session.context "" 3

        ( initial, _ ) =
            Session.init ctx

        ( privateAnonymous, _, _ ) =
            Session.update ctx (Session.SessionLoaded 3 1 (Ok anonymousSession)) initial

        ( withHandle, _, _ ) =
            Session.update ctx (Session.HandleChanged "owner") privateAnonymous

        ( withPassword, _, _ ) =
            Session.update ctx (Session.PasswordChanged "temporary") withHandle

        ( requested, _, _ ) =
            Session.update ctx Session.LoginRequested withPassword

        ( signedIn, _, outputs ) =
            Session.update ctx (Session.LoginCompleted 3 requested.generation (Ok adminSession)) requested
    in
    Session.canRead signedIn
        && Session.canWrite signedIn
        && Session.canModerate signedIn
        && Session.isAdmin signedIn
        && signedIn.session
        == Just adminSession
        && signedIn.passwordDraft
        == ""
        && outputs
        == [ Session.SessionChanged ]


caseStaleResponse : Bool
caseStaleResponse =
    let
        ctx =
            Session.context "" 9

        ( initial, _ ) =
            Session.init ctx

        ( next, _, _ ) =
            Session.update ctx (Session.SessionLoaded 8 1 (Ok adminSession)) initial
    in
    next == initial && not (Session.canRead next)


caseAccountPasswordClearing : Bool
caseAccountPasswordClearing =
    let
        ctx =
            Session.context "" 5

        ( initial, _ ) =
            Session.init ctx

        ( admin, _, _ ) =
            Session.update ctx (Session.SessionLoaded 5 1 (Ok adminSession)) initial

        ( withHandle, _, _ ) =
            Session.update ctx (Session.CreateHandleChanged "new-user") admin

        ( withPassword, _, _ ) =
            Session.update ctx (Session.CreatePasswordChanged "one-time-secret") withHandle

        ( requested, _, _ ) =
            Session.update ctx Session.UserCreateRequested withPassword

        ( completed, _, outputs ) =
            Session.update ctx (Session.UserCreated 5 requested.operationId (Ok newUser)) requested
    in
    requested.createPasswordDraft
        == ""
        && requested.createHandleDraft
        == ""
        && requested.usersBusy
        && completed.users
        == [ newUser ]
        && completed.createPasswordDraft
        == ""
        && not (String.contains "one-time-secret" (Maybe.withDefault "" completed.usersNotice))
        && outputs
        == [ Session.SessionChanged ]


caseViewerCannotManage : Bool
caseViewerCannotManage =
    let
        ctx =
            Session.context "" 6

        ( initial, _ ) =
            Session.init ctx

        ( viewer, _, _ ) =
            Session.update ctx (Session.SessionLoaded 6 1 (Ok viewerSession)) initial

        ( requested, _, _ ) =
            Session.update ctx Session.UsersRequested viewer
    in
    requested == viewer && not requested.usersBusy


caseCloseClearsDraft : Bool
caseCloseClearsDraft =
    let
        ctx =
            Session.context "" 2

        ( initial, _ ) =
            Session.init ctx

        ( opened, _, _ ) =
            Session.update ctx Session.LoginOpened initial

        ( withPassword, _, _ ) =
            Session.update ctx (Session.PasswordChanged "must-clear") opened

        ( closed, _, _ ) =
            Session.update ctx Session.LoginClosed withPassword
    in
    closed.passwordDraft == "" && not closed.loginOpen


anonymousSession : Domain.Session
anonymousSession =
    { privateMode = True
    , authenticated = False
    , user = Nothing
    , canRead = False
    , canWrite = False
    , canModerate = False
    , isAdmin = False
    }


adminSession : Domain.Session
adminSession =
    { privateMode = True
    , authenticated = True
    , user = Just { id = "12", handle = "owner", role = Administrator }
    , canRead = True
    , canWrite = True
    , canModerate = True
    , isAdmin = True
    }


viewerSession : Domain.Session
viewerSession =
    { privateMode = True
    , authenticated = True
    , user = Just { id = "13", handle = "viewer", role = Viewer }
    , canRead = True
    , canWrite = False
    , canModerate = False
    , isAdmin = False
    }


newUser : Domain.ManagedUser
newUser =
    { id = "21", handle = "new-user", role = Viewer, disabled = False }


adminJSON : String
adminJSON =
    """{"private_mode":true,"authenticated":true,"user":{"id":12,"handle":"owner","role":"admin"},"can_read":true,"can_write":true,"can_moderate":true,"is_admin":true}"""
