module Feature.Session exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , accessRevoked
    , canModerate
    , canRead
    , canWrite
    , context
    , withOrigin
    , init
    , isAdmin
    , AuthPage(..)
    , NoticeKind(..)
    , InvitationState(..)
    , authPage
    , viewAuth
    , privateMode
    , status
    , update
    , viewAccount
    , viewGate
    )

import Api.Error as ApiError
import Api.Session as Api
import Domain.Session as Domain exposing (Invitation, ManagedUser, Role(..), Session)
import Html exposing (Html, a, button, details, div, form, h1, h2, input, label, li, option, p, section, select, span, summary, text, ul)
import Html.Attributes exposing (attribute, class, disabled, href, name, placeholder, type_, value)
import Html.Attributes as Attr
import Html.Events exposing (onClick, onInput, onSubmit)
import Http
import String
import Url.Builder
import Ui.Icon


type alias Context =
    { apiBase : String
    , accessEpoch : Int
    , origin : String
    }


context : String -> Int -> Context
context apiBase accessEpoch =
    { apiBase = apiBase, accessEpoch = accessEpoch, origin = "" }


withOrigin : String -> Context -> Context
withOrigin origin ctx =
    { ctx | origin = origin }


type alias Model =
    { privateMode : Maybe Bool
    , session : Maybe Session
    , loading : Bool
    , generation : Int
    , loginOpen : Bool
    , handleDraft : String
    , passwordDraft : String
    , loginPasswordVisible : Bool
    , loginBusy : Bool
    , logoutBusy : Bool
    , notice : Maybe String
    , users : List ManagedUser
    , usersLoaded : Bool
    , usersBusy : Bool
    , usersNotice : Maybe String
    , createHandleDraft : String
    , createPasswordDraft : String
    , createRoleDraft : Role
    , userOperation : Maybe Int
    , operationId : Int
    , authPage : Maybe AuthPage
    , invitationToken : Maybe String
    , invitationPreview : InvitationState
    , signupHandleDraft : String
    , signupPasswordDraft : String
    , signupConfirmDraft : String
    , signupPasswordVisible : Bool
    , signupConfirmVisible : Bool
    , signupBusy : Bool
    , signupNotice : Maybe ( String, NoticeKind )
    , signupGeneration : Int
    , invitations : List Invitation
    , invitationsLoaded : Bool
    , invitationBusy : Bool
    , invitationRoleDraft : Role
    , invitationLink : Maybe String
    , invitationOperation : Maybe Int
    , reconcilePending : Bool
    , reconcileSignupPending : Bool
    , seenAccessEpoch : Int
    , passwordChangeOpen : Bool
    , currentPasswordDraft : String
    , newPasswordDraft : String
    , confirmPasswordDraft : String
    , passwordChangeBusy : Bool
    , passwordChangeOperation : Maybe Int
    , passwordChangeNotice : Maybe String
    }


init : Context -> ( Model, Cmd Msg )
init ctx =
    let
        model =
            { privateMode = Nothing
            , session = Nothing
            , loading = True
            , generation = 1
            , loginOpen = False
            , handleDraft = ""
            , passwordDraft = ""
            , loginPasswordVisible = False
            , loginBusy = False
            , logoutBusy = False
            , notice = Nothing
            , users = []
            , usersLoaded = False
            , usersBusy = False
            , usersNotice = Nothing
            , createHandleDraft = ""
            , createPasswordDraft = ""
            , createRoleDraft = Viewer
            , userOperation = Nothing
            , operationId = 0
            , authPage = Nothing
            , invitationToken = Nothing
            , invitationPreview = InvitationMissing
            , signupHandleDraft = ""
            , signupPasswordDraft = ""
            , signupConfirmDraft = ""
            , signupPasswordVisible = False
            , signupConfirmVisible = False
            , signupBusy = False
            , signupNotice = Nothing
            , signupGeneration = 0
            , invitations = []
            , invitationsLoaded = False
            , invitationBusy = False
            , invitationRoleDraft = Viewer
            , invitationLink = Nothing
            , invitationOperation = Nothing
            , reconcilePending = False
            , reconcileSignupPending = False
            , seenAccessEpoch = ctx.accessEpoch
            , passwordChangeOpen = False
            , currentPasswordDraft = ""
            , newPasswordDraft = ""
            , confirmPasswordDraft = ""
            , passwordChangeBusy = False
            , passwordChangeOperation = Nothing
            , passwordChangeNotice = Nothing
            }
    in
    ( model, Api.read ctx.apiBase (SessionLoaded ctx.accessEpoch model.generation) )


type Msg
    = SessionLoaded Int Int (Result Http.Error Session)
    | RefreshRequested
    | LoginOpened
    | LoginClosed
    | HandleChanged String
    | PasswordChanged String
    | PasswordVisibilityToggled
    | SignupPasswordVisibilityToggled
    | SignupConfirmVisibilityToggled
    | LoginRequested
    | LoginCompleted Int Int (Result Http.Error Session)
    | LogoutRequested
    | LogoutCompleted Int Int (Result Http.Error Session)
    | PasswordChangeOpened
    | PasswordChangeClosed
    | CurrentPasswordChanged String
    | NewPasswordChanged String
    | ConfirmPasswordChanged String
    | PasswordChangeRequested
    | PasswordChangeCompleted Int Int (Result Http.Error Session)
    | AccessRevokedRequested
    | UsersRequested
    | UsersLoaded Int Int (Result Http.Error (List ManagedUser))
    | CreateHandleChanged String
    | CreatePasswordChanged String
    | CreateRoleChanged String
    | UserCreateRequested
    | UserCreated Int Int (Result Http.Error ManagedUser)
    | UserRoleRequested String Role
    | UserDisabledRequested ManagedUser
    | UserUpdated Int Int String (Result Http.Error ManagedUser)
    | AuthPageChanged (Maybe AuthPage)
    | InvitationCaptured String
    | InvitationPreviewLoaded Int Int (Result Http.Error Api.InvitationPreview)
    | SignupHandleChanged String
    | SignupPasswordChanged String
    | SignupConfirmChanged String
    | SignupRequested
    | SignupCompleted Int Int (Result Http.Error Session)
    | InvitationsRequested
    | InvitationsLoaded Int Int (Result Http.Error (List Invitation))
    | InvitationRoleChanged String
    | InvitationCreateRequested
    | InvitationCreated Int Int (Result Http.Error ( Invitation, String ))
    | InvitationRevokeRequested String
    | InvitationRevoked Int Int String (Result Http.Error ())


type OutMsg
    = SessionChanged
    | AccessRevoked
    | Authenticated
    | AuthenticationRequired


type AuthPage
    = SignInPage
    | SignUpPage
    | TroublePage


type InvitationState
    = InvitationMissing
    | InvitationChecking
    | InvitationValid Api.InvitationPreview
    | InvitationRejected
    | InvitationUnavailable


type NoticeKind
    = ErrorNotice
    | SuccessNotice
    | InfoNotice


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg rawModel =
    let
        model =
            syncAccess ctx rawModel
    in
    case msg of
        SessionLoaded accessEpoch generation result ->
            if not (isCurrent ctx accessEpoch generation model) then
                ( model, Cmd.none, [] )

            else
                settleSessionRead result model

        RefreshRequested ->
            let
                nextGeneration =
                    model.generation + 1
            in
            ( { model | generation = nextGeneration, loading = True, notice = Nothing }
            , Api.read ctx.apiBase (SessionLoaded ctx.accessEpoch nextGeneration)
            , []
            )

        LoginOpened ->
            ( { model | loginOpen = True, handleDraft = "", passwordDraft = "", notice = Nothing }, Cmd.none, [] )

        LoginClosed ->
            ( { model | loginOpen = False, handleDraft = "", passwordDraft = "", notice = Nothing }, Cmd.none, [] )

        HandleChanged value ->
            ( { model | handleDraft = value, notice = Nothing }, Cmd.none, [] )

        PasswordChanged value ->
            ( { model | passwordDraft = value, notice = Nothing }, Cmd.none, [] )

        PasswordVisibilityToggled ->
            ( { model | loginPasswordVisible = not model.loginPasswordVisible }, Cmd.none, [] )

        LoginRequested ->
            if model.loginBusy then
                ( model, Cmd.none, [] )

            else if String.trim model.handleDraft == "" || model.passwordDraft == "" then
                ( { model | notice = Just "Enter your handle and password." }, Cmd.none, [] )

            else
                let
                    generation =
                        model.generation + 1

                    command =
                        Api.login ctx.apiBase (String.trim model.handleDraft) model.passwordDraft (LoginCompleted ctx.accessEpoch generation)
                in
                ( { model | generation = generation, passwordDraft = "", loginBusy = True, notice = Nothing }
                , command
                , []
                )

        LoginCompleted accessEpoch generation result ->
            if not (isCurrent ctx accessEpoch generation model) then
                if accessEpoch == ctx.accessEpoch then
                    reconcileStaleWrite ctx result model

                else
                    ( model, Cmd.none, [] )

            else
                settleLogin result model

        LogoutRequested ->
            if model.logoutBusy || not (isAuthenticated model) then
                ( model, Cmd.none, [] )

            else
                let
                    generation =
                        model.generation + 1
                in
                ( { model | generation = generation, logoutBusy = True, notice = Nothing }
                , Api.logout ctx.apiBase (LogoutCompleted ctx.accessEpoch generation)
                , []
                )

        LogoutCompleted accessEpoch generation result ->
            if not (isCurrent ctx accessEpoch generation model) then
                ( model, Cmd.none, [] )

            else
                settleLogout result model

        PasswordChangeOpened ->
            if not (isAuthenticated model) || model.passwordChangeBusy then
                ( model, Cmd.none, [] )

            else
                ( { model
                    | passwordChangeOpen = True
                    , currentPasswordDraft = ""
                    , newPasswordDraft = ""
                    , confirmPasswordDraft = ""
                    , passwordChangeNotice = Nothing
                  }
                , Cmd.none
                , []
                )

        PasswordChangeClosed ->
            ( clearPasswordChange model, Cmd.none, [] )

        CurrentPasswordChanged value ->
            ( { model | currentPasswordDraft = value, passwordChangeNotice = Nothing }, Cmd.none, [] )

        NewPasswordChanged value ->
            ( { model | newPasswordDraft = value, passwordChangeNotice = Nothing }, Cmd.none, [] )

        ConfirmPasswordChanged value ->
            ( { model | confirmPasswordDraft = value, passwordChangeNotice = Nothing }, Cmd.none, [] )

        PasswordChangeRequested ->
            if model.passwordChangeBusy || not (isAuthenticated model) then
                ( model, Cmd.none, [] )

            else if model.currentPasswordDraft == "" || model.newPasswordDraft == "" || model.confirmPasswordDraft == "" then
                ( { model | passwordChangeNotice = Just "Enter your current password and a new password." }, Cmd.none, [] )

            else if model.newPasswordDraft /= model.confirmPasswordDraft then
                ( { model | passwordChangeNotice = Just "New password and confirmation do not match." }, Cmd.none, [] )

            else
                let
                    id =
                        model.operationId + 1

                    currentPassword =
                        model.currentPasswordDraft

                    newPassword =
                        model.newPasswordDraft
                in
                ( { model
                    | operationId = id
                    , passwordChangeOperation = Just id
                    , passwordChangeBusy = True
                    , currentPasswordDraft = ""
                    , newPasswordDraft = ""
                    , confirmPasswordDraft = ""
                    , passwordChangeNotice = Nothing
                  }
                , Api.changePassword ctx.apiBase currentPassword newPassword (PasswordChangeCompleted ctx.accessEpoch id)
                , []
                )

        PasswordChangeCompleted accessEpoch id result ->
            if accessEpoch /= ctx.accessEpoch || model.passwordChangeOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                settlePasswordChange result model

        AccessRevokedRequested ->
            let
                cleared =
                    clearSession model
            in
            ( { cleared | privateMode = Just True, generation = model.generation + 1, loading = False, notice = Just "Your session expired. Sign in again to continue." }
            , Cmd.none
            , [ AccessRevoked, SessionChanged ]
            )

        UsersRequested ->
            if not (isAdmin model) || model.usersBusy then
                ( model, Cmd.none, [] )

            else
                let
                    id =
                        model.operationId + 1
                in
                ( { model | usersBusy = True, usersNotice = Nothing, operationId = id, userOperation = Just id }
                , Api.users ctx.apiBase (UsersLoaded ctx.accessEpoch id)
                , []
                )

        UsersLoaded accessEpoch id result ->
            if accessEpoch /= ctx.accessEpoch || model.userOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok users ->
                        ( { model | users = users, usersLoaded = True, usersBusy = False, userOperation = Nothing, usersNotice = Nothing }, Cmd.none, [] )

                    Err error ->
                        userReadFailed error model

        CreateHandleChanged value ->
            ( { model | createHandleDraft = value, usersNotice = Nothing }, Cmd.none, [] )

        CreatePasswordChanged value ->
            ( { model | createPasswordDraft = value, usersNotice = Nothing }, Cmd.none, [] )

        CreateRoleChanged raw ->
            case roleFromName raw of
                Just role ->
                    ( { model | createRoleDraft = role, usersNotice = Nothing }, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        UserCreateRequested ->
            if not (isAdmin model) || model.usersBusy then
                ( model, Cmd.none, [] )

            else if String.trim model.createHandleDraft == "" || model.createPasswordDraft == "" then
                ( { model | usersNotice = Just "Enter a handle and temporary password." }, Cmd.none, [] )

            else
                let
                    id =
                        model.operationId + 1

                    payload =
                        { handle = String.trim model.createHandleDraft
                        , password = model.createPasswordDraft
                        , role = model.createRoleDraft
                        }
                in
                ( { model | usersBusy = True, operationId = id, userOperation = Just id, createHandleDraft = "", createPasswordDraft = "", usersNotice = Nothing }
                , Api.createUser ctx.apiBase payload (UserCreated ctx.accessEpoch id)
                , []
                )

        UserCreated accessEpoch id result ->
            if accessEpoch /= ctx.accessEpoch || model.userOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok user ->
                        ( { model | users = model.users ++ [ user ], usersLoaded = True, usersBusy = False, userOperation = Nothing, usersNotice = Just "Account created." }
                        , Cmd.none
                        , [ SessionChanged ]
                        )

                    Err error ->
                        userWriteFailed error model

        UserRoleRequested userId role ->
            updateUserRequested ctx userId { role = Just role, disabled = Nothing } model

        UserDisabledRequested user ->
            updateUserRequested ctx user.id { role = Nothing, disabled = Just (not user.disabled) } model

        UserUpdated accessEpoch id userId result ->
            if accessEpoch /= ctx.accessEpoch || model.userOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok user ->
                        ( { model
                            | users =
                                List.map
                                    (\existing ->
                                        if existing.id == userId then
                                            user

                                        else
                                            existing
                                    )
                                    model.users
                            , usersBusy = False
                            , userOperation = Nothing
                            , usersNotice = Just "Account updated."
                          }
                        , Cmd.none
                        , [ SessionChanged ]
                        )

                    Err error ->
                        userWriteFailed error model

        AuthPageChanged page ->
            let
                changed =
                    model.authPage /= page

                needsRead =
                    changed && not model.loading && (model.loginBusy || model.signupBusy)

                nextGeneration =
                    if needsRead then
                        model.generation + 1

                    else
                        model.generation

                next =
                    { model
                        | authPage = page
                        , signupGeneration =
                            if changed then
                                model.signupGeneration + 1

                            else
                                model.signupGeneration
                        , signupPasswordDraft = ""
                        , signupConfirmDraft = ""
                        , signupBusy = False
                        , signupNotice = Nothing
                        , invitationToken =
                            if changed && page /= Just SignUpPage then
                                Nothing

                            else
                                model.invitationToken
                        , invitationPreview =
                            if changed && page /= Just SignUpPage then
                                InvitationMissing

                            else
                                model.invitationPreview
                        , loginBusy = False
                        , passwordDraft = ""
                        , loginPasswordVisible = False
                        , passwordChangeOpen = False
                        , currentPasswordDraft = ""
                        , newPasswordDraft = ""
                        , confirmPasswordDraft = ""
                        , passwordChangeBusy = False
                        , passwordChangeOperation = Nothing
                        , passwordChangeNotice = Nothing
                        , generation = nextGeneration
                        , loading = model.loading || needsRead
                    }
            in
            ( next
            , if needsRead then
                Api.read ctx.apiBase (SessionLoaded ctx.accessEpoch nextGeneration)

              else
                Cmd.none
            , []
            )

        InvitationCaptured token ->
            let
                generation =
                    model.signupGeneration + 1
            in
            if String.isEmpty token then
                ( { model | invitationToken = Nothing, invitationPreview = InvitationMissing, signupNotice = Nothing }, Cmd.none, [] )

            else
                ( { model
                    | invitationToken = Just token
                    , invitationPreview = InvitationChecking
                    , signupGeneration = generation
                    , signupNotice = Nothing
                  }
                , Api.previewInvitation ctx.apiBase token (InvitationPreviewLoaded ctx.accessEpoch generation)
                , []
                )

        InvitationPreviewLoaded accessEpoch generation result ->
            if accessEpoch /= ctx.accessEpoch || generation /= model.signupGeneration then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok preview ->
                        ( { model | invitationPreview = InvitationValid preview, signupNotice = Nothing }, Cmd.none, [] )

                    Err (Http.BadStatus 410) ->
                        ( { model | invitationPreview = InvitationRejected, signupNotice = Nothing, signupPasswordDraft = "", signupConfirmDraft = "" }, Cmd.none, [] )

                    Err _ ->
                        ( { model | invitationPreview = InvitationUnavailable, signupNotice = Nothing, signupPasswordDraft = "", signupConfirmDraft = "" }, Cmd.none, [] )

        SignupHandleChanged raw ->
            ( { model | signupHandleDraft = raw, signupNotice = Nothing }, Cmd.none, [] )

        SignupPasswordChanged raw ->
            ( { model | signupPasswordDraft = raw, signupNotice = Nothing }, Cmd.none, [] )

        SignupConfirmChanged raw ->
            ( { model | signupConfirmDraft = raw, signupNotice = Nothing }, Cmd.none, [] )

        SignupPasswordVisibilityToggled ->
            ( { model | signupPasswordVisible = not model.signupPasswordVisible }, Cmd.none, [] )

        SignupConfirmVisibilityToggled ->
            ( { model | signupConfirmVisible = not model.signupConfirmVisible }, Cmd.none, [] )

        SignupRequested ->
            if model.signupBusy then
                ( model, Cmd.none, [] )

            else
                case ( model.invitationToken, model.invitationPreview ) of
                    ( Just token, InvitationValid _ ) ->
                        if String.trim model.signupHandleDraft == "" || model.signupPasswordDraft == "" then
                            ( { model | signupNotice = Just ( "Enter a handle and password.", ErrorNotice ) }, Cmd.none, [] )

                        else if model.signupPasswordDraft /= model.signupConfirmDraft then
                            ( { model | signupNotice = Just ( "Passwords do not match.", ErrorNotice ) }, Cmd.none, [] )

                        else
                            let
                                generation = model.signupGeneration + 1
                            in
                            ( { model | signupGeneration = generation, signupBusy = True, signupNotice = Nothing }
                            , Api.register ctx.apiBase token (String.trim model.signupHandleDraft) model.signupPasswordDraft (SignupCompleted ctx.accessEpoch generation)
                            , []
                            )

                    _ ->
                        ( { model | signupNotice = Just ( "You need a valid invitation to create an account.", ErrorNotice ) }, Cmd.none, [] )

        SignupCompleted accessEpoch generation result ->
            if accessEpoch /= ctx.accessEpoch || generation /= model.signupGeneration then
                reconcileStaleWrite ctx result model

            else
                case result of
                    Ok session ->
                        if session.authenticated && session.canRead then
                            ( applySession session
                                { model
                                    | signupBusy = False
                                    , signupPasswordDraft = ""
                                    , signupConfirmDraft = ""
                                    , signupNotice = Just ( "Account created. Opening your library…", SuccessNotice )
                                    , authPage = Nothing
                                }
                            , Cmd.none
                            , [ Authenticated ]
                            )

                        else
                            ( { model | signupBusy = False, signupPasswordDraft = "", signupConfirmDraft = "", signupNotice = Just ( "Account creation did not grant access to this library.", ErrorNotice ) }, Cmd.none, [] )

                    Err (Http.BadStatus 409) ->
                        ( { model | signupBusy = False, signupPasswordDraft = "", signupConfirmDraft = "", signupNotice = Just ( "That handle is already in use. Choose another handle.", ErrorNotice ) }, Cmd.none, [] )

                    Err (Http.BadStatus 410) ->
                        ( { model | signupBusy = False, invitationPreview = InvitationRejected, signupPasswordDraft = "", signupConfirmDraft = "", signupNotice = Just ( "This invitation is no longer valid. Ask your library administrator for a new one.", ErrorNotice ) }, Cmd.none, [] )

                    Err (Http.BadStatus 400) ->
                        ( { model | signupBusy = False, signupPasswordDraft = "", signupConfirmDraft = "", signupNotice = Just ( "Password must be 12–72 UTF-8 bytes. Check your handle and try again.", ErrorNotice ) }, Cmd.none, [] )

                    Err (Http.BadStatus 429) ->
                        ( { model | signupBusy = False, signupPasswordDraft = "", signupConfirmDraft = "", signupNotice = Just ( "Account creation is temporarily limited. Wait a little and try again.", ErrorNotice ) }, Cmd.none, [] )

                    Err error ->
                        if isAmbiguousSignupFailure error then
                            beginSignupReconciliation ctx model

                        else
                            ( { model | signupBusy = False, signupPasswordDraft = "", signupConfirmDraft = "", signupNotice = Just ( "Account creation could not be completed. Check your details and try again.", ErrorNotice ) }, Cmd.none, [] )

        InvitationsRequested ->
            if not (isAdmin model) || model.invitationBusy then
                ( model, Cmd.none, [] )

            else
                let
                    id = model.operationId + 1
                in
                ( { model | operationId = id, invitationOperation = Just id, invitationBusy = True, invitationLink = Nothing }
                , Api.invitations ctx.apiBase (InvitationsLoaded ctx.accessEpoch id)
                , []
                )

        InvitationsLoaded accessEpoch id result ->
            if accessEpoch /= ctx.accessEpoch || model.invitationOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok invitationsList ->
                        ( { model | invitations = invitationsList, invitationsLoaded = True, invitationBusy = False, invitationOperation = Nothing }, Cmd.none, [] )

                    Err _ ->
                        ( { model | invitationBusy = False, invitationOperation = Nothing, usersNotice = Just "Invitations could not be loaded. Check the connection and try again." }, Cmd.none, [] )

        InvitationRoleChanged raw ->
            case roleFromName raw of
                Just role ->
                    ( { model | invitationRoleDraft = role, usersNotice = Nothing }, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        InvitationCreateRequested ->
            if not (isAdmin model) || model.invitationBusy then
                ( model, Cmd.none, [] )

            else
                let
                    id = model.operationId + 1
                in
                ( { model | operationId = id, invitationOperation = Just id, invitationBusy = True, invitationLink = Nothing, usersNotice = Nothing }
                , Api.createInvitation ctx.apiBase { role = model.invitationRoleDraft } (InvitationCreated ctx.accessEpoch id)
                , []
                )

        InvitationCreated accessEpoch id result ->
            if accessEpoch /= ctx.accessEpoch || model.invitationOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok (invitation, token) ->
                        ( { model
                            | invitations = invitation :: model.invitations
                            , invitationsLoaded = True
                            , invitationBusy = False
                            , invitationOperation = Nothing
                            , invitationLink = Just ( (if ctx.origin == "" then "" else ctx.origin) ++ "/sign-up#invite=" ++ token )
                            , usersNotice = Just "Invitation created. Copy this one-time link and share it with the person you invited."
                          }
                        , Cmd.none
                        , []
                        )

                    Err _ ->
                        ( { model | invitationBusy = False, invitationOperation = Nothing, usersNotice = Just "Invitation could not be created. Try again." }, Cmd.none, [] )

        InvitationRevokeRequested invitationId ->
            if not (isAdmin model) || model.invitationBusy then
                ( model, Cmd.none, [] )

            else
                let
                    id = model.operationId + 1
                in
                ( { model | operationId = id, invitationOperation = Just id, invitationBusy = True, invitationLink = Nothing }
                , Api.deleteInvitation ctx.apiBase invitationId (InvitationRevoked ctx.accessEpoch id invitationId)
                , []
                )

        InvitationRevoked accessEpoch id invitationId result ->
            if accessEpoch /= ctx.accessEpoch || model.invitationOperation /= Just id then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok _ ->
                        ( { model
                            | invitations = List.map (\invitation -> if invitation.id == invitationId then { invitation | revoked = True } else invitation) model.invitations
                            , invitationBusy = False
                            , invitationOperation = Nothing
                            , usersNotice = Just "Invitation revoked."
                          }
                        , Cmd.none
                        , []
                        )

                    Err _ ->
                        ( { model | invitationBusy = False, invitationOperation = Nothing, usersNotice = Just "Invitation could not be revoked. Check its status and try again." }, Cmd.none, [] )


updateUserRequested : Context -> String -> Api.UpdateUser -> Model -> ( Model, Cmd Msg, List OutMsg )
updateUserRequested ctx userId patch model =
    if not (isAdmin model) || model.usersBusy then
        ( model, Cmd.none, [] )

    else
        let
            id =
                model.operationId + 1
        in
        ( { model | usersBusy = True, operationId = id, userOperation = Just id, usersNotice = Nothing }
        , Api.updateUser ctx.apiBase userId patch (UserUpdated ctx.accessEpoch id userId)
        , []
        )


settleSessionRead : Result Http.Error Session -> Model -> ( Model, Cmd Msg, List OutMsg )
settleSessionRead result model =
    case result of
        Ok session ->
            ( applySession session
                { model
                    | loading = False
                    , notice = Nothing
                    , reconcilePending = False
                    , reconcileSignupPending = False
                    , signupBusy = False
                    , signupPasswordDraft = if model.reconcileSignupPending then "" else model.signupPasswordDraft
                    , signupConfirmDraft = if model.reconcileSignupPending then "" else model.signupConfirmDraft
                    , signupNotice =
                        if model.reconcileSignupPending && not (session.authenticated && session.canRead) then
                            Just ( "Account creation could not be confirmed. Try signing in or reopen the invitation.", ErrorNotice )

                        else
                            model.signupNotice
                }
            , Cmd.none
            , if session.authenticated && session.canRead && model.authPage /= Nothing then
                [ Authenticated ]

              else if session.authenticated && session.canRead && model.reconcilePending then
                [ SessionChanged ]

              else if session.privateMode && not session.authenticated && model.authPage == Nothing then
                [ AuthenticationRequired ]

              else
                []
            )

        Err _ ->
            ( { model
                | loading = False
                , privateMode = Just True
                , session = Nothing
                , notice = Just "Access could not be checked. Retry when the server is available."
                , reconcilePending = False
                , reconcileSignupPending = False
                , signupBusy = False
                , signupPasswordDraft = if model.reconcileSignupPending then "" else model.signupPasswordDraft
                , signupConfirmDraft = if model.reconcileSignupPending then "" else model.signupConfirmDraft
                , signupNotice =
                    if model.reconcileSignupPending then
                        Just ( "Account creation could not be confirmed. Try signing in or reopen the invitation.", ErrorNotice )

                    else
                        model.signupNotice
              }
            , Cmd.none
            , []
            )


beginSignupReconciliation : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
beginSignupReconciliation ctx model =
    let
        generation =
            model.generation + 1
    in
    ( { model
        | generation = generation
        , loading = True
        , reconcilePending = True
        , reconcileSignupPending = True
        , signupBusy = False
        , signupPasswordDraft = ""
        , signupConfirmDraft = ""
        , signupNotice = Just ( "Checking whether the account was created…", InfoNotice )
      }
    , Api.read ctx.apiBase (SessionLoaded ctx.accessEpoch generation)
    , []
    )


isAmbiguousSignupFailure : Http.Error -> Bool
isAmbiguousSignupFailure error =
    case error of
        Http.Timeout ->
            True

        Http.NetworkError ->
            True

        Http.BadBody _ ->
            True

        Http.BadStatus statusCode ->
            statusCode >= 500

        Http.BadUrl _ ->
            False


reconcileStaleWrite : Context -> Result Http.Error Session -> Model -> ( Model, Cmd Msg, List OutMsg )
reconcileStaleWrite ctx result model =
    case result of
        Ok session ->
            if session.authenticated then
                let
                    generation = model.generation + 1
                in
                ( { model | generation = generation, loading = True, reconcilePending = True, reconcileSignupPending = False }
                , Api.read ctx.apiBase (SessionLoaded ctx.accessEpoch generation)
                , []
                )

            else
                ( model, Cmd.none, [] )

        Err _ ->
            ( model, Cmd.none, [] )


settleLogin : Result Http.Error Session -> Model -> ( Model, Cmd Msg, List OutMsg )
settleLogin result model =
    case result of
        Ok session ->
            if session.authenticated && session.canRead then
                ( applySession session { model | loginBusy = False, loginOpen = False, handleDraft = "", passwordDraft = "", loginPasswordVisible = False, notice = Nothing }
                , Cmd.none
                , if model.authPage /= Nothing then [ Authenticated ] else [ SessionChanged ]
                )

            else
                ( { model | loginBusy = False, passwordDraft = "", notice = Just "Handle or password is incorrect." }, Cmd.none, [] )

        Err (Http.BadStatus 429) ->
            ( { model | loginBusy = False, passwordDraft = "", notice = Just "Sign in is temporarily limited. Wait a little and try again." }, Cmd.none, [] )

        Err (Http.BadStatus 401) ->
            ( { model | loginBusy = False, passwordDraft = "", notice = Just "Handle or password is incorrect." }, Cmd.none, [] )

        Err _ ->
            ( { model | loginBusy = False, passwordDraft = "", notice = Just "Sign in could not be completed. Check the connection and try again." }, Cmd.none, [] )


settlePasswordChange : Result Http.Error Session -> Model -> ( Model, Cmd Msg, List OutMsg )
settlePasswordChange result model =
    case result of
        Ok session ->
            if not session.authenticated then
                ( applySession session
                    { model
                        | passwordChangeOpen = False
                        , passwordChangeBusy = False
                        , passwordChangeOperation = Nothing
                        , currentPasswordDraft = ""
                        , newPasswordDraft = ""
                        , confirmPasswordDraft = ""
                        , passwordChangeNotice = Nothing
                        , notice = Just "Password changed. Sign in again with your new password."
                    }
                , Cmd.none
                , [ SessionChanged ]
                )

            else
                ( { model
                    | passwordChangeBusy = False
                    , passwordChangeOperation = Nothing
                    , currentPasswordDraft = ""
                    , newPasswordDraft = ""
                    , confirmPasswordDraft = ""
                    , passwordChangeNotice = Just "Password change could not be confirmed. Sign in to check. If the new password does not work, the previous password is still in effect."
                  }
                , Cmd.none
                , []
                )

        Err (Http.BadStatus 400) ->
            ( { model
                | passwordChangeBusy = False
                , passwordChangeOperation = Nothing
                , passwordChangeNotice = Just "The current password is incorrect, or the new password must be 12–72 bytes."
              }
            , Cmd.none
            , []
            )

        Err (Http.BadStatus 401) ->
            ( clearSession
                { model
                    | privateMode = Just True
                    , loading = False
                    , notice = Just "Your session expired. Sign in again to continue."
                    , passwordChangeNotice = Nothing
                }
            , Cmd.none
            , [ AccessRevoked, SessionChanged ]
            )

        Err (Http.BadStatus 403) ->
            ( { model
                | passwordChangeBusy = False
                , passwordChangeOperation = Nothing
                , passwordChangeNotice = Just "Password change was refused."
              }
            , Cmd.none
            , []
            )

        Err _ ->
            ( { model
                | passwordChangeBusy = False
                , passwordChangeOperation = Nothing
                , currentPasswordDraft = ""
                , newPasswordDraft = ""
                , confirmPasswordDraft = ""
                , passwordChangeNotice = Just "Password change could not be confirmed. Sign in to check. If the new password does not work, the previous password is still in effect."
              }
            , Cmd.none
            , []
            )


settleLogout : Result Http.Error Session -> Model -> ( Model, Cmd Msg, List OutMsg )
settleLogout result model =
    case result of
        Ok session ->
            if not session.authenticated then
                ( applySession session { model | logoutBusy = False, notice = Nothing }
                , Cmd.none
                , [ SessionChanged ]
                )

            else
                ( { model | logoutBusy = False, notice = Just "Sign out was not confirmed." }, Cmd.none, [] )

        Err _ ->
            ( { model | logoutBusy = False, notice = Just "Sign out could not be confirmed. Check the connection and try again." }, Cmd.none, [] )


userReadFailed : Http.Error -> Model -> ( Model, Cmd Msg, List OutMsg )
userReadFailed error model =
    case ApiError.classify error of
        ApiError.BadStatus 401 ->
            ( clearSession { model | usersBusy = False, userOperation = Nothing, users = [], usersLoaded = False, usersNotice = Nothing }
            , Cmd.none
            , [ AccessRevoked, SessionChanged ]
            )

        ApiError.BadStatus 429 ->
            ( { model | usersBusy = False, userOperation = Nothing, usersNotice = Just "Account management is temporarily limited." }, Cmd.none, [] )

        _ ->
            ( { model | usersBusy = False, userOperation = Nothing, usersNotice = Just "Accounts could not be loaded. Check the connection and try again." }, Cmd.none, [] )


userWriteFailed : Http.Error -> Model -> ( Model, Cmd Msg, List OutMsg )
userWriteFailed error model =
    case ApiError.classify error of
        ApiError.BadStatus 401 ->
            ( clearSession { model | usersBusy = False, userOperation = Nothing, usersNotice = Nothing }
            , Cmd.none
            , [ AccessRevoked, SessionChanged ]
            )

        ApiError.BadStatus 429 ->
            ( { model | usersBusy = False, userOperation = Nothing, usersNotice = Just "Account changes are temporarily limited." }, Cmd.none, [] )

        _ ->
            ( { model | usersBusy = False, userOperation = Nothing, usersNotice = Just "Account change could not be confirmed. Review the account list before trying again." }, Cmd.none, [] )


applySession : Session -> Model -> Model
applySession session model =
    let
        identityChanged =
            case model.session of
                Nothing ->
                    session.authenticated

                Just previous ->
                    previous.authenticated /= session.authenticated
                        || Maybe.map .id previous.user /= Maybe.map .id session.user
    in
    { model
        | privateMode = Just session.privateMode
        , session = Just session
        , loading = False
        , generation = model.generation + 1
        , users =
            if session.isAdmin then
                model.users

            else
                []
        , usersLoaded =
            if session.isAdmin then
                model.usersLoaded

            else
                False
        , usersBusy = False
        , userOperation = Nothing
        , passwordDraft = ""
        , loginPasswordVisible = False
        , passwordChangeOpen = False
        , currentPasswordDraft = ""
        , newPasswordDraft = ""
        , confirmPasswordDraft = ""
        , passwordChangeBusy = False
        , passwordChangeOperation = Nothing
        , passwordChangeNotice = Nothing
        , signupPasswordDraft = ""
        , signupConfirmDraft = ""
        , signupPasswordVisible = False
        , signupConfirmVisible = False
        , signupBusy = False
        , signupGeneration =
            if identityChanged then
                model.signupGeneration + 1

            else
                model.signupGeneration
        , invitationToken =
            if identityChanged then
                Nothing

            else
                model.invitationToken
        , invitationPreview =
            if identityChanged then
                InvitationMissing

            else
                model.invitationPreview
        , invitationLink =
            if identityChanged then
                Nothing

            else
                model.invitationLink
    }


syncAccess : Context -> Model -> Model
syncAccess ctx model =
    if model.seenAccessEpoch == ctx.accessEpoch then
        model

    else
        clearPasswordChange { model | seenAccessEpoch = ctx.accessEpoch }


clearPasswordChange : Model -> Model
clearPasswordChange model =
    { model
        | passwordChangeOpen = False
        , currentPasswordDraft = ""
        , newPasswordDraft = ""
        , confirmPasswordDraft = ""
        , passwordChangeBusy = False
        , passwordChangeOperation = Nothing
        , passwordChangeNotice = Nothing
    }


clearSession : Model -> Model
clearSession model =
    { model
        | session = Nothing
        , users = []
        , usersLoaded = False
        , usersBusy = False
        , userOperation = Nothing
        , loginBusy = False
        , logoutBusy = False
        , loginOpen = False
        , handleDraft = ""
        , passwordDraft = ""
        , createHandleDraft = ""
        , createPasswordDraft = ""
        , passwordChangeOpen = False
        , currentPasswordDraft = ""
        , newPasswordDraft = ""
        , confirmPasswordDraft = ""
        , passwordChangeBusy = False
        , passwordChangeOperation = Nothing
        , passwordChangeNotice = Nothing
        , loginPasswordVisible = False
        , signupPasswordDraft = ""
        , signupConfirmDraft = ""
        , signupPasswordVisible = False
        , signupConfirmVisible = False
        , signupBusy = False
        , invitationToken = Nothing
        , invitationPreview = InvitationMissing
        , invitationLink = Nothing
        , invitations = []
        , invitationsLoaded = False
        , invitationBusy = False
        , invitationOperation = Nothing
        , signupGeneration = model.signupGeneration + 1
    }


isCurrent : Context -> Int -> Int -> Model -> Bool
isCurrent ctx accessEpoch generation model =
    accessEpoch == ctx.accessEpoch && generation == model.generation


isAuthenticated : Model -> Bool
isAuthenticated model =
    model.session |> Maybe.map .authenticated |> Maybe.withDefault False


privateMode : Model -> Bool
privateMode model =
    model.privateMode == Just True


canRead : Model -> Bool
canRead model =
    model.session |> Maybe.map .canRead |> Maybe.withDefault False


canWrite : Model -> Bool
canWrite model =
    model.session |> Maybe.map .canWrite |> Maybe.withDefault False


canModerate : Model -> Bool
canModerate model =
    model.session |> Maybe.map .canModerate |> Maybe.withDefault False


isAdmin : Model -> Bool
isAdmin model =
    model.session |> Maybe.map .isAdmin |> Maybe.withDefault False


status : Model -> Maybe String
status model =
    model.notice


accessRevoked : Model -> Model
accessRevoked model =
    clearSession { model | privateMode = Just True, loading = False, generation = model.generation + 1 }


viewGate : Model -> Maybe (Html Msg)
viewGate model =
    if model.privateMode == Just False || canRead model then
        Nothing

    else
        Just <|
            section [ class "session-gate" ]
                [ h2 [] [ text "This library is private" ]
                , p [] [ text (Maybe.withDefault (if model.loading then "Checking access…" else "Sign in to browse this library.") model.notice) ]
                , button
                    [ type_ "button"
                    , onClick
                        (if model.loading then
                            RefreshRequested

                         else
                            LoginOpened
                        )
                    ]
                    [ text
                        (if model.loading then
                            "Check access"

                         else if model.privateMode == Just True then
                            "Sign in"

                         else
                            "Retry"
                        )
                    ]
                , if model.loginOpen then
                    loginForm model

                  else
                    text ""
                ]


viewAccount : Context -> Model -> Html Msg
viewAccount _ model =
    case model.session of
        Just session ->
            if session.authenticated then
                div [ class "session-account" ]
                    [ details [ class "account-disclosure" ]
                        [ summary [ class "account-disclosure-summary" ]
                            [ span [] [ text (Maybe.withDefault "Signed in" (Maybe.map .handle session.user)) ]
                            , session.user
                                |> Maybe.map (\user -> span [ class "session-role" ] [ text (Domain.roleName user.role) ])
                                |> Maybe.withDefault (text "")
                            ]
                        , div [ class "account-disclosure-body" ]
                            [ passwordChangeControl model
                            , button [ class "button button-quiet", type_ "button", disabled model.logoutBusy, onClick LogoutRequested ]
                                [ text
                                    (if model.logoutBusy then
                                        "Signing out…"

                                     else
                                        "Sign out"
                                    )
                                ]
                            , if isAdmin model then
                                adminAccounts model

                              else
                                text ""
                            ]
                        ]
                    , model.notice |> Maybe.map (\message -> p [ class "session-notice" ] [ text message ]) |> Maybe.withDefault (text "")
                    , model.usersNotice |> Maybe.map (\message -> p [ class "session-notice" ] [ text message ]) |> Maybe.withDefault (text "")
                    ]

            else
                text ""

        Nothing ->
            text ""


authPage : Model -> Maybe AuthPage
authPage =
    .authPage


viewAuth : Model -> AuthPage -> String -> Html Msg
viewAuth model page returnHref =
    let
        route hrefPath =
            href (Url.Builder.absolute [ hrefPath ] [ Url.Builder.string "return" returnHref ])

        notice message kind =
            p
                [ class
                    ("auth-message "
                        ++ (case kind of
                                ErrorNotice ->
                                    "is-error"

                                SuccessNotice ->
                                    "is-success"

                                InfoNotice ->
                                    "is-info"
                           )
                    )
                , attribute "role" (if kind == ErrorNotice then "alert" else "status")
                , attribute "aria-live" (if kind == ErrorNotice then "assertive" else "polite")
                ]
                [ if kind == ErrorNotice then
                    span [ class "auth-message-icon", attribute "aria-hidden" "true" ] [ Ui.Icon.view "alert-circle" ]

                  else
                    text ""
                , span [] [ text message ]
                ]

        safeImage =
            Html.node "img"
                [ class "auth-art"
                , Attr.src "/static/brand/auth-observatory.png"
                , Attr.alt ""
                , attribute "aria-hidden" "true"
                ]
                []
    in
    div [ class "auth-main" ]
        [ div [ class "auth-art-frame" ] [ safeImage ]
        , section [ class "auth-content" ]
            [ case page of
                SignInPage ->
                    form [ class "auth-form auth-sign-in", onSubmit LoginRequested ]
                        [ h1 [ class "auth-title" ] [ text "Sign in" ]
                        , p [ class "auth-subtitle" ] [ text "Continue to your library." ]
                        , authField "Handle" "handle" "text" "username" model.handleDraft model.loginBusy HandleChanged "Your handle"
                        , passwordField "auth-password" "Password" "password" "current-password" model.passwordDraft model.loginBusy model.loginPasswordVisible PasswordChanged PasswordVisibilityToggled
                        , a [ class "auth-help", route "trouble" ] [ text "Trouble signing in?" ]
                        , model.notice |> Maybe.map (\message -> notice message ErrorNotice) |> Maybe.withDefault (text "")
                        , button [ class "auth-submit", type_ "submit", disabled model.loginBusy ]
                            [ text (if model.loginBusy then "Signing in…" else "Sign in") ]
                        , div [ class "auth-links" ]
                            [ p [] [ text "New here? ", a [ route "sign-up" ] [ text "Create account" ] ]
                            , p [ class "auth-note" ] [ text "Access is provided by your library administrator." ]
                            ]
                        ]

                SignUpPage ->
                    signupContent route notice model

                TroublePage ->
                    div [ class "auth-trouble" ]
                        [ a [ class "auth-back", route "sign-in" ] [ Ui.Icon.view "arrow-left", text "Sign in" ]
                        , h1 [ class "auth-title" ] [ text "Trouble signing in?" ]
                        , p [] [ text "Ask your library administrator to confirm your handle or reset your account. Kura does not send password reset emails." ]
                        , a [ class "auth-submit auth-back-button", route "sign-in" ] [ text "Back to sign in" ]
                        ]
            ]
        ]


signupContent : (String -> Html.Attribute Msg) -> (String -> NoticeKind -> Html Msg) -> Model -> Html Msg
signupContent route notice model =
    case model.invitationPreview of
        InvitationMissing ->
            div [ class "auth-trouble" ]
                [ a [ class "auth-back", route "sign-in" ] [ Ui.Icon.view "arrow-left", text "Sign in" ]
                , h1 [ class "auth-title" ] [ text "Create account" ]
                , h2 [] [ text "You’ll need an invitation." ]
                , p [] [ text "Ask your library administrator to invite you. Once you have the link, you can choose your handle and password." ]
                , a [ class "auth-submit auth-back-button", route "sign-in" ] [ text "Back to sign in" ]
                ]

        InvitationChecking ->
            div [ class "auth-form" ] [ h1 [ class "auth-title" ] [ text "Create account" ], p [ class "auth-message is-info", attribute "role" "status" ] [ text "Checking your invitation…" ] ]

        InvitationRejected ->
            div [ class "auth-trouble" ]
                [ a [ class "auth-back", route "sign-in" ] [ Ui.Icon.view "arrow-left", text "Sign in" ]
                , h1 [ class "auth-title" ] [ text "Invitation no longer valid" ]
                , p [] [ text "This invitation has expired, been revoked, or already been used. Ask your library administrator for a new invitation." ]
                , a [ class "auth-submit auth-back-button", route "sign-in" ] [ text "Back to sign in" ]
                ]

        InvitationUnavailable ->
            div [ class "auth-trouble" ]
                [ a [ class "auth-back", route "sign-in" ] [ Ui.Icon.view "arrow-left", text "Sign in" ]
                , h1 [ class "auth-title" ] [ text "Invitation unavailable" ]
                , p [] [ text "We could not check this invitation. Check your connection and open the invitation link again." ]
                , a [ class "auth-submit auth-back-button", route "sign-in" ] [ text "Back to sign in" ]
                ]

        InvitationValid preview ->
            form [ class "auth-form auth-sign-up", onSubmit SignupRequested ]
                [ h1 [ class "auth-title" ] [ text "Create account" ]
                , p [ class "auth-subtitle" ] [ text ("Join " ++ preview.libraryName ++ "’s library.") ]
                , p [ class "auth-access" ] [ text ("Your access: " ++ roleTitle preview.role) ]
                , authField "Handle" "new-handle" "text" "username" model.signupHandleDraft model.signupBusy SignupHandleChanged "Choose a handle"
                , passwordField "auth-signup-password" "Password" "new-password" "new-password" model.signupPasswordDraft model.signupBusy model.signupPasswordVisible SignupPasswordChanged SignupPasswordVisibilityToggled
                , p [ class "auth-field-hint" ] [ text "Use a long, unique password." ]
                , passwordField "auth-confirm-password" "Confirm password" "new-password" "new-password" model.signupConfirmDraft model.signupBusy model.signupConfirmVisible SignupConfirmChanged SignupConfirmVisibilityToggled
                , model.signupNotice |> Maybe.map (\(message, kind) -> notice message kind) |> Maybe.withDefault (text "")
                , button [ class "auth-submit", type_ "submit", disabled model.signupBusy ] [ text (if model.signupBusy then "Creating account…" else "Create account") ]
                , div [ class "auth-links" ] [ p [] [ text "Already have an account? ", a [ route "sign-in" ] [ text "Sign in" ] ], p [ class "auth-note" ] [ text "Your library administrator manages your access." ] ]
                ]


authField : String -> String -> String -> String -> String -> Bool -> (String -> Msg) -> String -> Html Msg
authField labelText fieldName inputType autocompleteText draft busy changed hint =
    let
        fieldId = "auth-" ++ fieldName
    in
    div [ class "auth-field" ]
        [ label [ Attr.for fieldId ] [ text labelText ]
        , input
            [ Attr.id fieldId
            , type_ inputType
            , name fieldName
            , attribute "autocomplete" autocompleteText
            , value draft
            , placeholder hint
            , disabled busy
            , onInput changed
            ]
            []
        ]


passwordField : String -> String -> String -> String -> String -> Bool -> Bool -> (String -> Msg) -> Msg -> Html Msg
passwordField fieldId labelText fieldName autocompleteValue draft busy visible changed toggle =
    div [ class "auth-field" ]
        [ label [ Attr.for fieldId ] [ text labelText ]
        , div [ class "auth-password-control" ]
            [ input [ Attr.id fieldId, type_ (if visible then "text" else "password"), name fieldName, attribute "autocomplete" autocompleteValue, value draft, disabled busy, onInput changed ] []
            , button [ class "auth-password-toggle", type_ "button", attribute "aria-label" (if visible then "Hide password" else "Show password"), attribute "aria-pressed" (if visible then "true" else "false"), disabled busy, onClick toggle ] [ Ui.Icon.view (if visible then "eye-off" else "eye") ]
            ]
        ]


roleTitle : Role -> String
roleTitle role =
    case role of
        Viewer ->
            "Viewer"

        Editor ->
            "Editor"

        Administrator ->
            "Administrator"

        UnknownRole raw ->
            raw


passwordChangeControl : Model -> Html Msg
passwordChangeControl model =
    if model.passwordChangeOpen then
        form [ class "session-create-user", onSubmit PasswordChangeRequested ]
            [ p [] [ text "Changing your password signs you out everywhere. You will need to sign in again." ]
            , label []
                [ text "Current password"
                , input
                    [ type_ "password"
                    , name "current-password"
                    , attribute "autocomplete" "current-password"
                    , value model.currentPasswordDraft
                    , disabled model.passwordChangeBusy
                    , onInput CurrentPasswordChanged
                    ]
                    []
                ]
            , label []
                [ text "New password"
                , input
                    [ type_ "password"
                    , name "new-password"
                    , attribute "autocomplete" "new-password"
                    , value model.newPasswordDraft
                    , disabled model.passwordChangeBusy
                    , onInput NewPasswordChanged
                    ]
                    []
                ]
            , label []
                [ text "Confirm new password"
                , input
                    [ type_ "password"
                    , name "confirm-password"
                    , attribute "autocomplete" "new-password"
                    , value model.confirmPasswordDraft
                    , disabled model.passwordChangeBusy
                    , onInput ConfirmPasswordChanged
                    ]
                    []
                ]
            , model.passwordChangeNotice |> Maybe.map (\message -> p [ class "session-notice" ] [ text message ]) |> Maybe.withDefault (text "")
            , button [ type_ "submit", disabled model.passwordChangeBusy ]
                [ text
                    (if model.passwordChangeBusy then
                        "Changing password…"

                     else
                        "Change password"
                    )
                ]
            , button [ type_ "button", disabled model.passwordChangeBusy, onClick PasswordChangeClosed ] [ text "Close" ]
            ]

    else
        button [ class "button button-quiet", type_ "button", onClick PasswordChangeOpened ] [ text "Change password" ]


loginForm : Model -> Html Msg
loginForm model =
    form [ class "session-login", onSubmit LoginRequested ]
        [ label []
            [ text "Handle"
            , input [ type_ "text", name "handle", attribute "autocomplete" "username", value model.handleDraft, placeholder "Handle", disabled model.loginBusy, onInput HandleChanged ] []
            ]
        , label []
            [ text "Password"
            , input [ type_ "password", name "password", attribute "autocomplete" "current-password", value model.passwordDraft, placeholder "Password", disabled model.loginBusy, onInput PasswordChanged ] []
            ]
        , button [ type_ "submit", disabled model.loginBusy ]
            [ text
                (if model.loginBusy then
                    "Signing in…"

                 else
                    "Sign in"
                )
            ]
        , button [ type_ "button", disabled model.loginBusy, onClick LoginClosed ] [ text "Close" ]
        ]


adminAccounts : Model -> Html Msg
adminAccounts model =
    details [ class "session-admin" ]
        [ summary [] [ text "Accounts" ]
        , button [ type_ "button", disabled model.usersBusy, onClick UsersRequested ]
            [ text
                (if model.usersBusy then
                    "Working…"

                 else
                    "Load accounts"
                )
            ]
        , if model.usersLoaded then
            div [] (List.map (accountRow model) model.users)

          else
            text ""
        , form [ class "session-create-user", onSubmit UserCreateRequested ]
            [ label []
                [ text "New handle"
                , input [ type_ "text", name "new-handle", attribute "autocomplete" "off", value model.createHandleDraft, placeholder "Handle", disabled model.usersBusy, onInput CreateHandleChanged ] []
                ]
            , label []
                [ text "Temporary password"
                , input [ type_ "password", name "new-password", attribute "autocomplete" "new-password", value model.createPasswordDraft, placeholder "Temporary password", disabled model.usersBusy, onInput CreatePasswordChanged ] []
                ]
            , label []
                [ text "Role"
                , select [ value (Domain.roleName model.createRoleDraft), disabled model.usersBusy, onInput CreateRoleChanged ]
                    [ roleOption Viewer
                    , roleOption Editor
                    , roleOption Administrator
                    ]
                ]
            , button [ type_ "submit", disabled model.usersBusy ] [ text "Create account" ]
            ]
        , adminInvitations model
        ]


adminInvitations : Model -> Html Msg
adminInvitations model =
    section [ class "auth-admin" ]
        [ h2 [] [ text "Invitations" ]
        , p [ class "auth-note" ] [ text "Create a one-time link and share it yourself. Links expire after seven days." ]
        , form [ class "auth-invitation-form", onSubmit InvitationCreateRequested ]
            [ label []
                [ text "Access level"
                , select [ value (Domain.roleName model.invitationRoleDraft), disabled model.invitationBusy, onInput InvitationRoleChanged ]
                    [ roleOption Viewer, roleOption Editor, roleOption Administrator ]
                ]
            , button [ type_ "submit", disabled model.invitationBusy ] [ text (if model.invitationBusy then "Creating…" else "Create invitation") ]
            ]
        , case model.invitationLink of
            Just link ->
                p [ class "auth-message is-success", attribute "role" "status" ] [ a [ href link ] [ text link ] ]

            Nothing ->
                text ""
        , button [ type_ "button", disabled model.invitationBusy, onClick InvitationsRequested ]
            [ text (if model.invitationBusy then "Working…" else "Load invitations") ]
        , if model.invitationsLoaded then
            ul [ class "auth-invitation-list" ] (List.map (invitationRow model) model.invitations)

          else
            text ""
        ]


invitationRow : Model -> Invitation -> Html Msg
invitationRow model invitation =
    li [ class "auth-invitation-row" ]
        [ div [ class "auth-invitation-meta" ]
            [ strongText (roleTitle invitation.role)
            , span [] [ text (if invitation.revoked then "Revoked" else if invitation.used then "Used" else "Available") ]
            , span [] [ text ("Expires " ++ invitation.expiresAt) ]
            ]
        , if invitation.revoked || invitation.used then
            text ""

          else
            div [ class "auth-invitation-actions" ]
                [ button [ type_ "button", disabled model.invitationBusy, onClick (InvitationRevokeRequested invitation.id) ] [ text "Revoke" ] ]
        ]


strongText : String -> Html Msg
strongText raw =
    Html.strong [] [ text raw ]


accountRow : Model -> ManagedUser -> Html Msg
accountRow model user =
    div [ class "session-user-row" ]
        [ span [] [ text user.handle ]
        , select
            [ value (Domain.roleName user.role)
            , disabled (model.usersBusy || user.disabled)
            , onInput (\raw -> roleFromName raw |> Maybe.map (UserRoleRequested user.id) |> Maybe.withDefault RefreshRequested)
            ]
            [ roleOption Viewer, roleOption Editor, roleOption Administrator ]
        , span []
            [ text
                (if user.disabled then
                    "Disabled"

                 else
                    "Active"
                )
            ]
        , button [ type_ "button", disabled model.usersBusy, onClick (UserDisabledRequested user) ]
            [ text
                (if user.disabled then
                    "Enable"

                 else
                    "Disable"
                )
            ]
        ]


roleOption : Role -> Html Msg
roleOption role =
    option [ value (Domain.roleName role) ] [ text (Domain.roleName role) ]


roleFromName : String -> Maybe Role
roleFromName raw =
    case raw of
        "viewer" ->
            Just Viewer

        "editor" ->
            Just Editor

        "admin" ->
            Just Administrator

        _ ->
            Nothing
