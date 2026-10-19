module Feature.Profile exposing
    ( Model
    , Msg(..)
    , OutMsg(..)
    , pageHref
    , init
    , isDirty
    , open
    , openOwn
    , retarget
    , route
    , title
    , update
    , view
    )

import Api.Collection
import Api.Profile as Api
import App.Route as Route
import Domain.Collection exposing (CollectionAck)
import Domain.Profile as Profile exposing (Draft, Profile)
import File exposing (File)
import File.Select
import Html exposing (Html, a, button, div, form, h1, h2, header, img, input, label, p, span, text, textarea)
import Html.Attributes exposing (alt, attribute, class, disabled, href, maxlength, placeholder, rel, rows, src, target, type_, value)
import Html.Events exposing (onClick, onInput, onSubmit)
import Http
import Task


type Request
    = ByName String
    | Rejected
    | ResolvingOwn


type alias Model =
    { request : Request
    , generation : Int
    , profile : Maybe Profile
    , lists : List Api.ProfileList
    , nextCursor : Maybe String
    , listsFailed : Bool
    , editing : Maybe Draft
    , notice : Maybe String
    , pending : Bool
    , listName : String
    , creatingList : Bool
    , previewUrl : Maybe String
    }


type Msg
    = ProfileReceived Int (Result Http.Error Profile)
    | ListsReceived Int (Result Http.Error Api.ListPage)
    | EditOpened
    | DisplayNameChanged String
    | UsernameChanged String
    | BioChanged String
    | WebsiteChanged String
    | SaveRequested
    | SaveCompleted (Result Api.Failure Profile)
    | CancelEdit
    | PickPicture
    | PicturePicked File
    | PreviewReady String
    | AvatarCompleted (Result Api.Failure Profile)
    | RemovePicture
    | ListNameChanged String
    | CreateList
    | ListCreated (Result Http.Error CollectionAck)
    | MoreLists
    | Retry


type OutMsg
    = NavigateTo String


init : Model
init =
    { request = Rejected
    , generation = 0
    , profile = Nothing
    , lists = []
    , nextCursor = Nothing
    , listsFailed = False
    , editing = Nothing
    , notice = Nothing
    , pending = False
    , listName = ""
    , creatingList = False
    , previewUrl = Nothing
    }


open : String -> Route.Route -> ( Model, Cmd Msg )
open apiBase libraryRoute =
    case Route.profileOf libraryRoute of
        Route.Profile name ->
            load apiBase (ByName name) init

        Route.RejectedProfile ->
            ( { init | request = Rejected }, Cmd.none )

        Route.NoProfile ->
            ( init, Cmd.none )


retarget : String -> Route.Route -> Model -> ( Model, Cmd Msg )
retarget apiBase libraryRoute model =
    case Route.profileOf libraryRoute of
        Route.Profile name ->
            if model.request == ByName name && (model.profile /= Nothing || model.pending) then
                ( model, Cmd.none )

            else
                load apiBase (ByName name) model

        Route.RejectedProfile ->
            ( { init | request = Rejected }, Cmd.none )

        Route.NoProfile ->
            ( init, Cmd.none )


load : String -> Request -> Model -> ( Model, Cmd Msg )
load apiBase request model =
    let
        generation =
            model.generation + 1

        next =
            { init | request = request, generation = generation, pending = True }
    in
    case request of
        ByName name ->
            ( next, Api.view apiBase name (ProfileReceived generation) )

        ResolvingOwn ->
            ( next, Api.own apiBase (ProfileReceived generation) )

        Rejected ->
            ( { next | pending = False }, Cmd.none )


openOwn : String -> Model -> ( Model, Cmd Msg )
openOwn apiBase model =
    load apiBase ResolvingOwn model


pageHref : Model -> Maybe String
pageHref model =
    case model.request of
        ByName name ->
            Just ("/u/" ++ name)

        _ ->
            Nothing


route : Model -> Maybe Route.Route
route model =
    case model.request of
        ByName name ->
            Just (Route.forProfile name)

        _ ->
            Nothing


title : Model -> String
title model =
    case model.profile of
        Just profile ->
            heading profile ++ " · Kura"

        Nothing ->
            if model.pending then
                "Profile · Kura"

            else
                "Profile not found · Kura"


isDirty : Model -> Bool
isDirty model =
    case ( model.profile, model.editing ) of
        ( Just profile, Just draft ) ->
            Profile.isDirty profile draft || model.previewUrl /= Nothing

        _ ->
            False


update : String -> Bool -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update apiBase canWrite msg model =
    case msg of
        ProfileReceived generation result ->
            if generation /= model.generation then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok profile ->
                        let
                            loaded =
                                { model
                                    | pending = False
                                    , profile = Just profile
                                    , notice = Nothing
                                    , request = ByName profile.username
                                    , lists = []
                                    , nextCursor = Nothing
                                    , listsFailed = False
                                }

                            outs =
                                case model.request of
                                    ResolvingOwn ->
                                        [ NavigateTo ("/u/" ++ profile.username) ]

                                    _ ->
                                        []
                        in
                        ( loaded
                        , Api.lists apiBase profile.username Nothing (ListsReceived generation)
                        , outs
                        )

                    Err (Http.BadStatus 404) ->
                        ( { model | pending = False, profile = Nothing, notice = Nothing }, Cmd.none, [] )

                    Err _ ->
                        ( { model | pending = False, profile = Nothing, notice = Just "The profile could not be loaded." }, Cmd.none, [] )

        ListsReceived generation result ->
            if generation /= model.generation then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        ( { model
                            | lists = model.lists ++ page.lists
                            , nextCursor = page.nextCursor
                            , listsFailed = False
                          }
                        , Cmd.none
                        , []
                        )

                    Err _ ->
                        ( { model | listsFailed = True }, Cmd.none, [] )

        EditOpened ->
            case model.profile of
                Just profile ->
                    if profile.canEdit then
                        ( { model | editing = Just (Profile.draftFrom profile), notice = Nothing }, Cmd.none, [] )

                    else
                        ( model, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        DisplayNameChanged value ->
            ( mapDraft (\draft -> { draft | displayName = value }) model, Cmd.none, [] )

        UsernameChanged value ->
            ( mapDraft (\draft -> { draft | username = value }) model, Cmd.none, [] )

        BioChanged value ->
            ( mapDraft (\draft -> { draft | bio = value }) model, Cmd.none, [] )

        WebsiteChanged value ->
            ( mapDraft (\draft -> { draft | website = value }) model, Cmd.none, [] )

        SaveRequested ->
            case ( model.profile, model.editing ) of
                ( Just profile, Just draft ) ->
                    if profile.canEdit && not model.pending then
                        case validate draft of
                            Ok ready ->
                                ( { model | pending = True, notice = Nothing }
                                , Api.save apiBase ready SaveCompleted
                                , []
                                )

                            Err message ->
                                ( { model | notice = Just message }, Cmd.none, [] )

                    else
                        ( model, Cmd.none, [] )

                _ ->
                    ( model, Cmd.none, [] )

        SaveCompleted result ->
            let
                saved =
                    finishEdit result model

                outs =
                    case result of
                        Ok profile ->
                            if model.request /= ByName profile.username then
                                [ NavigateTo ("/u/" ++ profile.username) ]

                            else
                                []

                        Err _ ->
                            []
            in
            ( saved, Cmd.none, outs )

        CancelEdit ->
            ( { model | editing = Nothing, previewUrl = Nothing, notice = Nothing }, Cmd.none, [] )

        PickPicture ->
            ( model, File.Select.file [ "image/jpeg", "image/png", "image/webp" ] PicturePicked, [] )

        PicturePicked file ->
            case model.profile of
                Just profile ->
                    if profile.canEdit && not model.pending then
                        ( { model | pending = True, notice = Nothing }
                        , Cmd.batch
                            [ Task.perform PreviewReady (File.toUrl file)
                            , Api.replaceAvatar apiBase (draftVersion model profile) file AvatarCompleted
                            ]
                        , []
                        )

                    else
                        ( model, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        PreviewReady url ->
            ( { model | previewUrl = Just url }, Cmd.none, [] )

        AvatarCompleted result ->
            ( finishAvatar result model, Cmd.none, [] )

        RemovePicture ->
            case model.profile of
                Just profile ->
                    if profile.canEdit && profile.avatarUrl /= "" && not model.pending then
                        ( { model | pending = True, previewUrl = Nothing, notice = Nothing }
                        , Api.clearAvatar apiBase (draftVersion model profile) AvatarCompleted
                        , []
                        )

                    else
                        ( { model | previewUrl = Nothing }, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        ListNameChanged value ->
            ( { model | listName = value }, Cmd.none, [] )

        CreateList ->
            case model.profile of
                Just profile ->
                    if profile.canEdit && canWrite && not model.creatingList && String.trim model.listName /= "" then
                        ( { model | creatingList = True, notice = Nothing }
                        , Api.Collection.create apiBase Nothing (String.trim model.listName) ListCreated
                        , []
                        )

                    else
                        ( model, Cmd.none, [] )

                Nothing ->
                    ( model, Cmd.none, [] )

        ListCreated result ->
            case ( result, model.profile ) of
                ( Ok _ , Just profile ) ->
                    ( { model | creatingList = False, listName = "", lists = [], nextCursor = Nothing }
                    , Api.lists apiBase profile.username Nothing (ListsReceived model.generation)
                    , []
                    )

                ( Err _, _ ) ->
                    ( { model | creatingList = False, notice = Just "The list could not be created." }, Cmd.none, [] )

                ( Ok _, Nothing ) ->
                    ( { model | creatingList = False }, Cmd.none, [] )

        MoreLists ->
            case ( model.profile, model.nextCursor ) of
                ( Just profile, Just cursor ) ->
                    ( model, Api.lists apiBase profile.username (Just cursor) (ListsReceived model.generation), [] )

                _ ->
                    ( model, Cmd.none, [] )

        Retry ->
            case model.request of
                ByName name ->
                    load apiBase (ByName name) model |> withNoOut

                ResolvingOwn ->
                    openOwn apiBase model |> withNoOut

                Rejected ->
                    ( model, Cmd.none, [] )


view : Bool -> Model -> Html Msg
view canWrite model =
    div [ class "profile-page" ]
        [ case model.profile of
            Nothing ->
                if model.pending then
                    p [ class "profile-status" ] [ text "Loading profile…" ]

                else if model.request == Rejected || model.notice == Nothing then
                    div [ class "profile-missing" ]
                        [ h1 [] [ text "Profile not found" ]
                        , p [] [ text "That username is not a profile." ]
                        ]

                else
                    div [ class "profile-missing" ]
                        [ h1 [] [ text "Profile unavailable" ]
                        , p [] [ text (Maybe.withDefault "The profile could not be loaded." model.notice) ]
                        , button [ class "button", type_ "button", onClick Retry ] [ text "Retry" ]
                        ]

            Just profile ->
                div []
                    [ header [ class "profile-header" ]
                        [ avatarView model profile
                        , div [ class "profile-identity" ]
                            [ h1 [] [ text (heading profile) ]
                            , p [ class "profile-username" ] [ text ("@" ++ profile.username) ]
                            , if profile.bio == "" then
                                text ""

                              else
                                p [ class "profile-bio" ] [ text profile.bio ]
                            , websiteLink profile.website
                            , if profile.canEdit && model.editing == Nothing then
                                button [ class "button", type_ "button", onClick EditOpened ] [ text "Edit profile" ]

                              else
                                text ""
                            ]
                        ]
                    , case model.editing of
                        Just draft ->
                            editForm model draft

                        Nothing ->
                            text ""
                    , noticeView model.notice
                    , h2 [ class "profile-lists-title" ] [ text "Lists" ]
                    , listsView canWrite model profile
                    ]
        ]


avatarView : Model -> Profile -> Html Msg
avatarView model profile =
    let
        picture =
            case model.previewUrl of
                Just url ->
                    Just url

                Nothing ->
                    if profile.avatarUrl == "" then
                        Nothing

                    else
                        Just profile.avatarUrl
    in
    case picture of
        Just url ->
            img [ class "profile-avatar", src url, alt "" ] []

        Nothing ->
            div [ class "profile-avatar profile-avatar-fallback", attribute "aria-hidden" "true" ]
                [ text (Profile.initials (heading profile)) ]


editForm : Model -> Draft -> Html Msg
editForm model draft =
    form [ class "profile-edit", onSubmit SaveRequested ]
        [ label []
            [ span [] [ text "Display name" ]
            , input [ type_ "text", maxlength 50, value draft.displayName, onInput DisplayNameChanged ] []
            ]
        , label []
            [ span [] [ text "Username" ]
            , input [ type_ "text", maxlength 32, value draft.username, onInput UsernameChanged ] []
            ]
        , label []
            [ span [] [ text "Bio" ]
            , textarea [ rows 3, maxlength 160, value draft.bio, onInput BioChanged ] []
            ]
        , label []
            [ span [] [ text "Website" ]
            , input [ type_ "url", maxlength 200, placeholder "https://", value draft.website, onInput WebsiteChanged ] []
            ]
        , p [ class "profile-hint" ] [ text "Pictures are cropped to a square from the center." ]
        , div [ class "profile-edit-actions" ]
            [ button [ class "button", type_ "button", disabled model.pending, onClick PickPicture ] [ text "Upload picture" ]
            , button [ class "button button-quiet", type_ "button", disabled model.pending, onClick RemovePicture ] [ text "Remove picture" ]
            , button [ class "button button-primary", type_ "submit", disabled model.pending ] [ text (if model.pending then "Saving…" else "Save") ]
            , button [ class "button button-quiet", type_ "button", disabled model.pending, onClick CancelEdit ] [ text "Cancel" ]
            ]
        ]


listsView : Bool -> Model -> Profile -> Html Msg
listsView canWrite model profile =
    div [ class "profile-lists" ]
        [ if model.listsFailed then
            p [ class "profile-status" ] [ text "Lists could not be loaded." ]

          else if List.isEmpty model.lists && model.nextCursor == Nothing then
            p [ class "profile-status" ]
                [ text
                    (if profile.canEdit && canWrite then
                        "No lists yet. Create one to start a personal collection."

                     else
                        "No lists yet."
                    )
                ]

          else
            div [ class "profile-list-grid" ] (List.map listCard model.lists)
        , if profile.canEdit && canWrite then
            form [ class "profile-create-list", onSubmit CreateList ]
                [ input [ type_ "text", placeholder "New list", value model.listName, onInput ListNameChanged ] []
                , button [ class "button", type_ "submit", disabled model.creatingList ] [ text "Create list" ]
                ]

          else
            text ""
        , case model.nextCursor of
            Just _ ->
                button [ class "button button-quiet", type_ "button", onClick MoreLists ] [ text "More lists" ]

            Nothing ->
                text ""
        ]


listCard : Api.ProfileList -> Html Msg
listCard item =
    let
        destination =
            Route.forCollection item.id
                |> Maybe.map Route.toHref
                |> Maybe.withDefault "/"
    in
    a [ class "profile-list-card", href destination ]
        [ case Profile.safeCover item.coverUrl of
            Just cover ->
                img [ class "profile-list-cover", src cover, alt "" ] []

            Nothing ->
                div [ class "profile-list-cover profile-list-cover-empty" ] []
        , span [ class "profile-list-name" ] [ text item.name ]
        , span [ class "profile-list-count" ]
            [ text
                (if item.visibleCount == 1 then
                    "1 item"

                 else
                    String.fromInt item.visibleCount ++ " items"
                )
            ]
        ]


websiteLink : String -> Html msg
websiteLink raw =
    if raw == "" then
        text ""

    else
        p [ class "profile-website" ]
            [ a [ href raw, target "_blank", rel "noopener noreferrer" ] [ text raw ] ]


noticeView : Maybe String -> Html msg
noticeView notice =
    case notice of
        Just message ->
            p [ class "profile-notice", attribute "role" "alert" ] [ text message ]

        Nothing ->
            text ""


heading : Profile -> String
heading profile =
    if String.trim profile.displayName == "" then
        profile.username

    else
        profile.displayName


mapDraft : (Draft -> Draft) -> Model -> Model
mapDraft change model =
    { model | editing = Maybe.map change model.editing }


draftVersion : Model -> Profile -> Int
draftVersion model profile =
    model.editing |> Maybe.map .expectedVersion |> Maybe.withDefault profile.version


validate : Draft -> Result String Draft
validate draft =
    case Profile.normalizeUsername draft.username of
        Err message ->
            Err message

        Ok username ->
            case Profile.validWebsite draft.website of
                Err message ->
                    Err message

                Ok website ->
                    if String.length (String.trim draft.displayName) > 50 || String.length (String.trim draft.bio) > 160 then
                        Err "profile is not valid"

                    else
                        Ok { draft | username = username, website = website, displayName = String.trim draft.displayName, bio = String.trim draft.bio }


finishEdit : Result Api.Failure Profile -> Model -> Model
finishEdit result model =
    case result of
        Ok profile ->
            { model | pending = False, profile = Just profile, editing = Nothing, previewUrl = Nothing, notice = Nothing, request = ByName profile.username }

        Err (Api.Conflict profile) ->
            { model
                | pending = False
                , profile = Just { profile | displayName = draftText .displayName profile.displayName model, bio = draftText .bio profile.bio model, website = draftText .website profile.website model }
                , editing = Maybe.map (Profile.applyRemoteVersion profile) model.editing
                , notice = Just (Api.failureMessage (Api.Conflict profile))
            }

        Err failure ->
            { model | pending = False, notice = Just (Api.failureMessage failure) }


finishAvatar : Result Api.Failure Profile -> Model -> Model
finishAvatar result model =
    case result of
        Ok profile ->
            { model
                | pending = False
                , previewUrl = Nothing
                , profile = Just profile
                , editing = Maybe.map (Profile.applyRemoteVersion profile) model.editing
                , notice = Nothing
            }

        Err (Api.Conflict profile) ->
            { model
                | pending = False
                , previewUrl = Nothing
                , profile = Just profile
                , editing = Maybe.map (Profile.applyRemoteVersion profile) model.editing
                , notice = Just (Api.failureMessage (Api.Conflict profile))
            }

        Err failure ->
            { model | pending = False, previewUrl = Nothing, notice = Just (Api.failureMessage failure) }


draftText : (Draft -> String) -> String -> Model -> String
draftText pick fallback model =
    model.editing |> Maybe.map pick |> Maybe.withDefault fallback


withNoOut : ( Model, Cmd Msg ) -> ( Model, Cmd Msg, List OutMsg )
withNoOut ( model, cmd ) =
    ( model, cmd, [] )
