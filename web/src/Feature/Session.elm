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
    , init
    , isAdmin
    , privateMode
    , status
    , update
    , viewAccount
    , viewGate
    )

import Api.Error as ApiError
import Api.Session as Api
import Domain.Session as Domain exposing (ManagedUser, Role(..), Session)
import Html exposing (Html, button, details, div, form, h2, input, label, option, p, section, select, span, summary, text)
import Html.Attributes exposing (attribute, class, disabled, name, placeholder, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit)
import Http
import String


type alias Context =
    { apiBase : String
    , accessEpoch : Int
    }


context : String -> Int -> Context
context apiBase accessEpoch =
    { apiBase = apiBase, accessEpoch = accessEpoch }


type alias Model =
    { privateMode : Maybe Bool
    , session : Maybe Session
    , loading : Bool
    , generation : Int
    , loginOpen : Bool
    , handleDraft : String
    , passwordDraft : String
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
    | LoginRequested
    | LoginCompleted Int Int (Result Http.Error Session)
    | LogoutRequested
    | LogoutCompleted Int Int (Result Http.Error Session)
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


type OutMsg
    = SessionChanged
    | AccessRevoked


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
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
                ( { model | generation = generation, handleDraft = "", passwordDraft = "", loginBusy = True, notice = Nothing }
                , command
                , []
                )

        LoginCompleted accessEpoch generation result ->
            if not (isCurrent ctx accessEpoch generation model) then
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
            ( applySession session { model | loading = False, notice = Nothing }, Cmd.none, [] )

        Err _ ->
            ( { model | loading = False, privateMode = Just True, session = Nothing, notice = Just "Access could not be checked. Retry when the server is available." }, Cmd.none, [] )


settleLogin : Result Http.Error Session -> Model -> ( Model, Cmd Msg, List OutMsg )
settleLogin result model =
    case result of
        Ok session ->
            if session.authenticated && session.canRead then
                ( applySession session { model | loginBusy = False, loginOpen = False, handleDraft = "", passwordDraft = "", notice = Nothing }
                , Cmd.none
                , [ SessionChanged ]
                )

            else
                ( { model | loginBusy = False, handleDraft = "", passwordDraft = "", notice = Just "Sign in did not grant access to this library." }, Cmd.none, [] )

        Err (Http.BadStatus 429) ->
            ( { model | loginBusy = False, handleDraft = "", passwordDraft = "", notice = Just "Sign in is temporarily limited. Wait a little and try again." }, Cmd.none, [] )

        Err (Http.BadStatus 401) ->
            ( { model | loginBusy = False, handleDraft = "", passwordDraft = "", notice = Just "Handle or password was not accepted." }, Cmd.none, [] )

        Err _ ->
            ( { model | loginBusy = False, handleDraft = "", passwordDraft = "", notice = Just "Sign in could not be completed. Check the connection and try again." }, Cmd.none, [] )


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
    div [ class "session-account" ]
        [ accountSummary model
        , if isAdmin model then
            adminAccounts model

          else
            text ""
        , if model.loginOpen && privateMode model && not (isAuthenticated model) then
            loginForm model

          else
            text ""
        , model.notice |> Maybe.map (\message -> p [ class "session-notice" ] [ text message ]) |> Maybe.withDefault (text "")
        ]


accountSummary : Model -> Html Msg
accountSummary model =
    case model.session of
        Just session ->
            if session.authenticated then
                div [ class "session-account-summary" ]
                    [ span [] [ text (Maybe.withDefault "Signed in" (Maybe.map .handle session.user)) ]
                    , session.user |> Maybe.map (\user -> span [ class "session-role" ] [ text (Domain.roleName user.role) ]) |> Maybe.withDefault (text "")
                    , p [] [ text (permissionSummary session) ]
                    , button [ type_ "button", disabled model.logoutBusy, onClick LogoutRequested ]
                        [ text
                            (if model.logoutBusy then
                                "Signing out…"

                             else
                                "Sign out"
                            )
                        ]
                    ]

            else if session.privateMode then
                button [ type_ "button", onClick LoginOpened ] [ text "Sign in" ]

            else
                text ""

        Nothing ->
            if privateMode model then
                button [ type_ "button", onClick LoginOpened ] [ text "Sign in" ]

            else if model.loading then
                span [ class "session-loading" ] [ text "Checking access…" ]

            else
                text ""


permissionSummary : Session -> String
permissionSummary session =
    if session.isAdmin then
        "Read, edit, moderate, and account administration"

    else if session.canModerate then
        "Read, edit, and moderate"

    else if session.canWrite then
        "Read and edit"

    else if session.canRead then
        "Read only"

    else
        "No library access"


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
        , model.usersNotice |> Maybe.map (\message -> p [ class "session-notice" ] [ text message ]) |> Maybe.withDefault (text "")
        ]


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
