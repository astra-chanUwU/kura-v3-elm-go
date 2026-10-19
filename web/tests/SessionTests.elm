module SessionTests exposing (suite)

import Api.Session as Api
import Domain.Session as Domain exposing (Role(..))
import Feature.Session as Session
import Http
import Json.Decode as Decode
import Page.Library as Library
import String
import Url


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
    , ( "invitation endpoints are same origin", caseInvitationEndpoints )
    , ( "invitation redemption retains handle and returns authenticated", caseInvitationSignup )
    , ( "invalid invitation cannot submit account creation", caseInvalidInvitationStopsSignup )
    , ( "navigation ignores a late invitation preview and register response", caseStaleInvitationResponses )
    , ( "auth sign in reports authenticated without discarding its route", caseAuthLoginOutput )
    , ( "auth fragment is captured before URL cleanup", caseAuthFragmentCleanup )
    , ( "auth return accepts only the library route", caseSafeAuthReturn )
    , ( "authenticated session skips the standalone sign in page", caseExistingAuthRedirect )
    , ( "private anonymous session requests the standalone auth page", casePrivateAnonymousRedirect )
    , ( "stale signup success refreshes server session before library resumes", caseStaleSignupReconciliation )
    , ( "registration HTTP 400 explains password byte bounds", caseRegistrationBadRequest )
    , ( "ambiguous signup reconciles cookie before reporting failure", caseAmbiguousSignupReconciliation )
    , ( "session discovery failure stays unconfirmed and retryable", caseSessionDiscoveryFailure )
    , ( "initial anonymous session cannot stale the invite preview after fragment cleanup", caseInitialInvitePreviewSurvivesSessionDiscovery )
    , ( "password change endpoint does not target another account", casePasswordChangeEndpoint )
    , ( "viewer can open change password and drafts clear on submit and close", casePasswordChangeDrafts )
    , ( "password change drafts clear when access or session changes", casePasswordChangeClearsOnAccessChange )
    , ( "invalid expired and ambiguous password changes do not retry", casePasswordChangeDoesNotRetry )
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
                == Just { id = "12", handle = "owner", role = Administrator, username = Nothing }

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
        == "owner"
        && requested.loginBusy
        && failed.passwordDraft
        == ""
        && failed.handleDraft
        == "owner"
        && not failed.loginBusy
        && failed.notice
        == Just "Handle or password is incorrect."
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


caseInvitationEndpoints : Bool
caseInvitationEndpoints =
    Api.endpoint "" [ "api", "auth", "invitations", "preview" ]
        == "/api/auth/invitations/preview"
        && Api.endpoint "" [ "api", "auth", "register" ]
        == "/api/auth/register"
        && Api.endpoint "" [ "api", "auth", "invitations", "17" ]
        == "/api/auth/invitations/17"


caseInvitationSignup : Bool
caseInvitationSignup =
    let
        ctx =
            Session.context "" 22

        ( initial, _ ) =
            Session.init ctx

        ( routed, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignUpPage)) initial

        ( captured, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "opaque-token") routed

        ( previewReady, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 22 captured.signupGeneration (Ok { role = Viewer, expiresAt = "2026-10-23T00:00:00Z", libraryName = "Kura" })) captured

        ( withHandle, _, _ ) =
            Session.update ctx (Session.SignupHandleChanged "new-person") previewReady

        ( withPassword, _, _ ) =
            Session.update ctx (Session.SignupPasswordChanged "a-long-unique-password") withHandle

        ( ready, _, _ ) =
            Session.update ctx (Session.SignupConfirmChanged "a-long-unique-password") withPassword

        ( requested, _, _ ) =
            Session.update ctx Session.SignupRequested ready

        ( signedUp, _, outputs ) =
            Session.update ctx (Session.SignupCompleted 22 requested.signupGeneration (Ok viewerSession)) requested
    in
    requested.signupBusy
        && requested.signupHandleDraft
        == "new-person"
        && requested.signupPasswordDraft
        == "a-long-unique-password"
        && Session.canRead signedUp
        && signedUp.signupPasswordDraft
        == ""
        && signedUp.signupConfirmDraft
        == ""
        && outputs
        == [ Session.Authenticated ]


caseInvalidInvitationStopsSignup : Bool
caseInvalidInvitationStopsSignup =
    let
        ctx =
            Session.context "" 23

        ( initial, _ ) =
            Session.init ctx

        ( captured, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "bad-token") initial

        ( rejected, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 23 captured.signupGeneration (Err (Http.BadStatus 410))) captured

        ( requested, _, _ ) =
            Session.update ctx Session.SignupRequested rejected
    in
    not requested.signupBusy
        && requested.invitationToken
        == Just "bad-token"
        && requested.signupNotice
        == Just ( "You need a valid invitation to create an account.", Session.ErrorNotice )


caseStaleInvitationResponses : Bool
caseStaleInvitationResponses =
    let
        ctx =
            Session.context "" 24

        ( initial, _ ) =
            Session.init ctx

        ( captured, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "token") initial

        ( navigated, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.TroublePage)) captured

        ( latePreview, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 24 captured.signupGeneration (Ok { role = Viewer, expiresAt = "later", libraryName = "Kura" })) navigated

        ( signUp, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignUpPage)) latePreview

        ( capturedAgain, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "token-2") signUp

        ( ready, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 24 capturedAgain.signupGeneration (Ok { role = Viewer, expiresAt = "later", libraryName = "Kura" })) capturedAgain

        ( handle, _, _ ) =
            Session.update ctx (Session.SignupHandleChanged "person") ready

        ( password, _, _ ) =
            Session.update ctx (Session.SignupPasswordChanged "a-long-unique-password") handle

        ( confirm, _, _ ) =
            Session.update ctx (Session.SignupConfirmChanged "a-long-unique-password") password

        ( requested, _, _ ) =
            Session.update ctx Session.SignupRequested confirm

        ( navigatedAgain, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignInPage)) requested

        ( lateRegister, _, outputs ) =
            Session.update ctx (Session.SignupCompleted 24 requested.signupGeneration (Ok viewerSession)) navigatedAgain
    in
    latePreview.invitationPreview
        == Session.InvitationMissing
        && latePreview.invitationToken
        == Nothing
        && lateRegister.session
        == navigatedAgain.session
        && lateRegister.loading
        && lateRegister.generation
        == navigatedAgain.generation + 1
        && outputs
        == []


caseAuthLoginOutput : Bool
caseAuthLoginOutput =
    let
        ctx =
            Session.context "" 25

        ( initial, _ ) =
            Session.init ctx

        ( signIn, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignInPage)) initial

        ( withHandle, _, _ ) =
            Session.update ctx (Session.HandleChanged "owner") signIn

        ( withPassword, _, _ ) =
            Session.update ctx (Session.PasswordChanged "secret") withHandle

        ( requested, _, _ ) =
            Session.update ctx Session.LoginRequested withPassword

        ( authenticated, _, outputs ) =
            Session.update ctx (Session.LoginCompleted 25 requested.generation (Ok adminSession)) requested
    in
    Session.canRead authenticated
        && Session.authPage authenticated
        == Just Session.SignInPage
        && outputs
        == [ Session.Authenticated ]


caseAuthFragmentCleanup : Bool
caseAuthFragmentCleanup =
    case Url.fromString "https://kura.example/sign-up?return=%2F%3Fq%3Dnight#invite=opaque-token" of
        Nothing ->
            False

        Just url ->
            let
                ctx =
                    Session.context "" 26

                ( initial, _ ) =
                    Session.init ctx

                token =
                    Library.inviteTokenFromUrl url

                ( captured, _, _ ) =
                    Session.update ctx (Session.InvitationCaptured (Maybe.withDefault "" token)) initial

                cleaned =
                    Library.cleanAuthUrl url
            in
            token
                == Just "opaque-token"
                && cleaned
                == "/sign-up?return=%2F%3Fq%3Dnight"
                && captured.invitationToken
                == token


caseSafeAuthReturn : Bool
caseSafeAuthReturn =
    let
        result raw =
            Url.fromString ("https://kura.example/sign-in?return=" ++ raw)
                |> Maybe.map Library.authReturnFromUrl
    in
    result "%2F%3Fq%3Dnight" == Just "/?q=night"
        && result "%2Fsign-in" == Just "/"
        && result "%2Fapi%2Fauth%2Fsession" == Just "/"
        && result "%2F%2Fevil.example" == Just "/"


caseExistingAuthRedirect : Bool
caseExistingAuthRedirect =
    let
        ctx =
            Session.context "" 27

        ( initial, _ ) =
            Session.init ctx

        ( routed, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignInPage)) initial

        ( loaded, _, outputs ) =
            Session.update ctx (Session.SessionLoaded 27 1 (Ok adminSession)) routed
    in
    Session.canRead loaded
        && outputs
        == [ Session.Authenticated ]


casePrivateAnonymousRedirect : Bool
casePrivateAnonymousRedirect =
    let
        ctx =
            Session.context "" 28

        ( initial, _ ) =
            Session.init ctx

        ( anonymous, _, outputs ) =
            Session.update ctx (Session.SessionLoaded 28 1 (Ok anonymousSession)) initial
    in
    not (Session.canRead anonymous)
        && outputs
        == [ Session.AuthenticationRequired ]


caseStaleSignupReconciliation : Bool
caseStaleSignupReconciliation =
    let
        ctx =
            Session.context "" 29

        ( initial, _ ) =
            Session.init ctx

        ( signupPage, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignUpPage)) initial

        ( captured, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "token") signupPage

        ( previewReady, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 29 captured.signupGeneration (Ok { role = Viewer, expiresAt = "later", libraryName = "Kura" })) captured

        ( handle, _, _ ) =
            Session.update ctx (Session.SignupHandleChanged "person") previewReady

        ( password, _, _ ) =
            Session.update ctx (Session.SignupPasswordChanged "a-long-unique-password") handle

        ( confirm, _, _ ) =
            Session.update ctx (Session.SignupConfirmChanged "a-long-unique-password") password

        ( busy, _, _ ) =
            Session.update ctx (Session.SignupRequested) confirm

        ( navigated, _, _ ) =
            Session.update ctx (Session.AuthPageChanged Nothing) busy

        ( staleSuccess, _, _ ) =
            Session.update ctx (Session.SignupCompleted 29 busy.signupGeneration (Ok viewerSession)) navigated

        ( reconciled, _, outputs ) =
            Session.update ctx (Session.SessionLoaded 29 staleSuccess.generation (Ok viewerSession)) staleSuccess
    in
    staleSuccess.reconcilePending
        && Session.canRead reconciled
        && not reconciled.reconcilePending
        && outputs
        == [ Session.SessionChanged ]


caseRegistrationBadRequest : Bool
caseRegistrationBadRequest =
    let
        ctx =
            Session.context "" 30

        ( initial, _ ) =
            Session.init ctx

        ( captured, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "token") initial

        ( previewReady, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 30 captured.signupGeneration (Ok { role = Viewer, expiresAt = "later", libraryName = "Kura" })) captured

        ( handle, _, _ ) =
            Session.update ctx (Session.SignupHandleChanged "person") previewReady

        ( password, _, _ ) =
            Session.update ctx (Session.SignupPasswordChanged "short") handle

        ( confirm, _, _ ) =
            Session.update ctx (Session.SignupConfirmChanged "short") password

        ( requested, _, _ ) =
            Session.update ctx Session.SignupRequested confirm

        ( failed, _, _ ) =
            Session.update ctx (Session.SignupCompleted 30 requested.signupGeneration (Err (Http.BadStatus 400))) requested
    in
    failed.signupNotice
        == Just ( "Password must be 12–72 UTF-8 bytes. Check your handle and try again.", Session.ErrorNotice )
        && failed.signupHandleDraft
        == "person"
        && failed.signupPasswordDraft
        == ""


caseAmbiguousSignupReconciliation : Bool
caseAmbiguousSignupReconciliation =
    let
        ctx =
            Session.context "" 31

        ( initial, _ ) =
            Session.init ctx

        ( routed, _, _ ) =
            Session.update ctx (Session.AuthPageChanged (Just Session.SignUpPage)) initial

        ( captured, _, _ ) =
            Session.update ctx (Session.InvitationCaptured "opaque-token") routed

        ( previewReady, _, _ ) =
            Session.update ctx (Session.InvitationPreviewLoaded 31 captured.signupGeneration (Ok { role = Viewer, expiresAt = "later", libraryName = "Kura" })) captured

        ( withHandle, _, _ ) =
            Session.update ctx (Session.SignupHandleChanged "new-person") previewReady

        ( withPassword, _, _ ) =
            Session.update ctx (Session.SignupPasswordChanged "a-long-unique-password") withHandle

        ( ready, _, _ ) =
            Session.update ctx (Session.SignupConfirmChanged "a-long-unique-password") withPassword

        ( requested, _, _ ) =
            Session.update ctx Session.SignupRequested ready

        ( reconciling, _, _ ) =
            Session.update ctx (Session.SignupCompleted 31 requested.signupGeneration (Err (Http.BadStatus 503))) requested

        ( recoveredAnonymous, _, outputs ) =
            Session.update ctx (Session.SessionLoaded 31 reconciling.generation (Ok anonymousSession)) reconciling
    in
    reconciling.loading
        && reconciling.reconcileSignupPending
        && reconciling.signupHandleDraft
        == "new-person"
        && reconciling.signupPasswordDraft
        == ""
        && recoveredAnonymous.signupNotice
        == Just ( "Account creation could not be confirmed. Try signing in or reopen the invitation.", Session.ErrorNotice )
        && recoveredAnonymous.signupHandleDraft
        == "new-person"
        && not recoveredAnonymous.reconcileSignupPending
        && outputs
        == []


caseSessionDiscoveryFailure : Bool
caseSessionDiscoveryFailure =
    let
        ctx =
            Session.context "" 32

        ( initial, _ ) =
            Session.init ctx

        ( failed, _, outputs ) =
            Session.update ctx (Session.SessionLoaded 32 initial.generation (Err Http.NetworkError)) initial
    in
    not failed.loading
        && Session.privateMode failed
        && failed.session
        == Nothing
        && Session.status failed
        == Just "Access could not be checked. Retry when the server is available."
        && outputs
        == []


caseInitialInvitePreviewSurvivesSessionDiscovery : Bool
caseInitialInvitePreviewSurvivesSessionDiscovery =
    case Url.fromString "https://kura.example/sign-up#invite=revoked-token" of
        Nothing ->
            False

        Just url ->
            let
                ctx =
                    Session.context "" 33

                ( initial, _ ) =
                    Session.init ctx

                ( routed, _, _ ) =
                    Session.update ctx (Session.AuthPageChanged (Just Session.SignUpPage)) initial

                token =
                    Library.inviteTokenFromUrl url

                ( captured, _, _ ) =
                    Session.update ctx (Session.InvitationCaptured (Maybe.withDefault "" token)) routed

                ( routeReapplied, _, _ ) =
                    Session.update ctx (Session.AuthPageChanged (Just Session.SignUpPage)) captured

                cleanedUrl =
                    Library.cleanAuthUrl url

                ( discoveredAnonymous, _, _ ) =
                    Session.update ctx (Session.SessionLoaded 33 initial.generation (Ok anonymousSession)) routeReapplied

                ( rejected, _, _ ) =
                    Session.update ctx (Session.InvitationPreviewLoaded 33 captured.signupGeneration (Err (Http.BadStatus 410))) discoveredAnonymous
            in
            cleanedUrl
                == "/sign-up"
                && token
                == Just "revoked-token"
                && routeReapplied.signupGeneration
                == captured.signupGeneration
                && discoveredAnonymous.signupGeneration
                == captured.signupGeneration
                && rejected.invitationPreview
                == Session.InvitationRejected


casePasswordChangeEndpoint : Bool
casePasswordChangeEndpoint =
    Api.endpoint "" [ "api", "auth", "password" ]
        == "/api/auth/password"


casePasswordChangeDrafts : Bool
casePasswordChangeDrafts =
    let
        ctx =
            Session.context "" 40

        ( initial, _ ) =
            Session.init ctx

        ( viewer, _, _ ) =
            Session.update ctx (Session.SessionLoaded 40 1 (Ok viewerSession)) initial

        ( opened, _, _ ) =
            Session.update ctx Session.PasswordChangeOpened viewer

        ( current, _, _ ) =
            Session.update ctx (Session.CurrentPasswordChanged "current-secret") opened

        ( next, _, _ ) =
            Session.update ctx (Session.NewPasswordChanged "replacement-secret") current

        ( ready, _, _ ) =
            Session.update ctx (Session.ConfirmPasswordChanged "replacement-secret") next

        ( mismatch, _, _ ) =
            Session.update ctx Session.PasswordChangeRequested { ready | confirmPasswordDraft = "different-secret" }

        ( requested, _, _ ) =
            Session.update ctx Session.PasswordChangeRequested ready

        ( closed, _, _ ) =
            Session.update ctx Session.PasswordChangeClosed ready

        ( stillBusy, _, _ ) =
            Session.update ctx Session.PasswordChangeClosed requested

        ( managed, _, _ ) =
            Session.update ctx Session.UsersRequested viewer
    in
    opened.passwordChangeOpen
        && not (Session.isAdmin viewer)
        && managed
        == viewer
        && mismatch.currentPasswordDraft
        == "current-secret"
        && not mismatch.passwordChangeBusy
        && requested.passwordChangeBusy
        && requested.currentPasswordDraft
        == ""
        && requested.newPasswordDraft
        == ""
        && requested.confirmPasswordDraft
        == ""
        && closed.currentPasswordDraft
        == ""
        && closed.newPasswordDraft
        == ""
        && closed.confirmPasswordDraft
        == ""
        && not closed.passwordChangeOpen
        && stillBusy.passwordChangeOpen
        && stillBusy.passwordChangeBusy
        && not (String.contains "current-secret" (Maybe.withDefault "" requested.passwordChangeNotice))
        && not (String.contains "replacement-secret" (Maybe.withDefault "" closed.passwordChangeNotice))


casePasswordChangeClearsOnAccessChange : Bool
casePasswordChangeClearsOnAccessChange =
    let
        ctx =
            Session.context "" 41

        ( initial, _ ) =
            Session.init ctx

        ( viewer, _, _ ) =
            Session.update ctx (Session.SessionLoaded 41 1 (Ok viewerSession)) initial

        ( opened, _, _ ) =
            Session.update ctx Session.PasswordChangeOpened viewer

        ( current, _, _ ) =
            Session.update ctx (Session.CurrentPasswordChanged "current-secret") opened

        ( next, _, _ ) =
            Session.update ctx (Session.NewPasswordChanged "replacement-secret") current

        ( ready, _, _ ) =
            Session.update ctx (Session.ConfirmPasswordChanged "replacement-secret") next

        nextCtx =
            Session.context "" 42

        ( shifted, _, _ ) =
            Session.update nextCtx Session.RefreshRequested ready

        ( reopened, _, _ ) =
            Session.update ctx Session.PasswordChangeOpened viewer

        ( drafted, _, _ ) =
            Session.update ctx (Session.CurrentPasswordChanged "current-secret") reopened

        ( confirmed, _, _ ) =
            Session.update ctx (Session.NewPasswordChanged "replacement-secret") drafted

        ( filled, _, _ ) =
            Session.update ctx (Session.ConfirmPasswordChanged "replacement-secret") confirmed

        ( switched, _, _ ) =
            Session.update ctx (Session.SessionLoaded 41 filled.generation (Ok adminSession)) filled
    in
    shifted.currentPasswordDraft
        == ""
        && shifted.newPasswordDraft
        == ""
        && shifted.confirmPasswordDraft
        == ""
        && not shifted.passwordChangeOpen
        && switched.currentPasswordDraft
        == ""
        && switched.newPasswordDraft
        == ""
        && switched.confirmPasswordDraft
        == ""
        && not switched.passwordChangeOpen


casePasswordChangeDoesNotRetry : Bool
casePasswordChangeDoesNotRetry =
    let
        ctx =
            Session.context "" 43

        ( initial, _ ) =
            Session.init ctx

        ( viewer, _, _ ) =
            Session.update ctx (Session.SessionLoaded 43 1 (Ok viewerSession)) initial

        ( opened, _, _ ) =
            Session.update ctx Session.PasswordChangeOpened viewer

        ( current, _, _ ) =
            Session.update ctx (Session.CurrentPasswordChanged "current-secret") opened

        ( next, _, _ ) =
            Session.update ctx (Session.NewPasswordChanged "replacement-secret") current

        ( ready, _, _ ) =
            Session.update ctx (Session.ConfirmPasswordChanged "replacement-secret") next

        ( requested, _, _ ) =
            Session.update ctx Session.PasswordChangeRequested ready

        operation =
            Maybe.withDefault 0 requested.passwordChangeOperation

        ( invalid, _, invalidOutputs ) =
            Session.update ctx (Session.PasswordChangeCompleted 43 operation (Err (Http.BadStatus 400))) requested

        ( requestedAgain, _, _ ) =
            Session.update ctx Session.PasswordChangeRequested ready

        expiredOperation =
            Maybe.withDefault 0 requestedAgain.passwordChangeOperation

        ( expired, _, expiredOutputs ) =
            Session.update ctx (Session.PasswordChangeCompleted 43 expiredOperation (Err (Http.BadStatus 401))) requestedAgain

        ( requestedThird, _, _ ) =
            Session.update ctx Session.PasswordChangeRequested ready

        ambiguousOperation =
            Maybe.withDefault 0 requestedThird.passwordChangeOperation

        ( ambiguous, _, ambiguousOutputs ) =
            Session.update ctx (Session.PasswordChangeCompleted 43 ambiguousOperation (Err Http.Timeout)) requestedThird

        ( requestedFourth, _, _ ) =
            Session.update ctx Session.PasswordChangeRequested ready

        staleOperation =
            Maybe.withDefault 0 requestedFourth.passwordChangeOperation

        ( stale, _, staleOutputs ) =
            Session.update ctx (Session.PasswordChangeCompleted 99 staleOperation (Ok anonymousSession)) requestedFourth

        ( changed, _, changedOutputs ) =
            Session.update ctx (Session.PasswordChangeCompleted 43 staleOperation (Ok anonymousSession)) requestedFourth
    in
    not invalid.passwordChangeBusy
        && invalid.passwordChangeNotice
        == Just "The current password is incorrect, or the new password must be 12–72 bytes."
        && invalid.session
        == viewer.session
        && not invalid.loading
        && not invalid.reconcilePending
        && invalidOutputs
        == []
        && not (Session.canRead expired)
        && expired.currentPasswordDraft
        == ""
        && expiredOutputs
        == [ Session.AccessRevoked, Session.SessionChanged ]
        && not ambiguous.passwordChangeBusy
        && not ambiguous.reconcilePending
        && not ambiguous.loading
        && ambiguous.session
        == viewer.session
        && ambiguous.passwordChangeNotice
        == Just "Password change could not be confirmed. Sign in to check. If the new password does not work, the previous password is still in effect."
        && ambiguousOutputs
        == []
        && stale
        == requestedFourth
        && staleOutputs
        == []
        && not (Session.canRead changed)
        && changed.notice
        == Just "Password changed. Sign in again with your new password."
        && changed.currentPasswordDraft
        == ""
        && changed.newPasswordDraft
        == ""
        && changedOutputs
        == [ Session.SessionChanged ]


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
    , user = Just { id = "12", handle = "owner", role = Administrator, username = Nothing }
    , canRead = True
    , canWrite = True
    , canModerate = True
    , isAdmin = True
    }


viewerSession : Domain.Session
viewerSession =
    { privateMode = True
    , authenticated = True
    , user = Just { id = "13", handle = "viewer", role = Viewer, username = Nothing }
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
