module Main exposing (main)

import Api.Post
import Browser
import Browser.Events
import Browser.Navigation as Nav
import Domain.Post exposing (PostSummary, SearchResponse)
import Feature.MediaGrid
import Feature.QuickLook
import Feature.Selection
import Html exposing (Html, button, div, form, h1, header, input, main_, nav, p, section, text)
import Html.Attributes exposing (attribute, class, placeholder, type_, value)
import Html.Events exposing (on, onBlur, onClick, onFocus, onInput, onSubmit)
import Http
import Json.Decode as Decode
import Set exposing (Set)
import String
import Url
import Url.Builder


type alias Flags =
    { apiBase : String
    }


type alias Model =
    { key : Nav.Key
    , apiBase : String
    , query : String
    , draftQuery : String
    , posts : List PostSummary
    , loading : Bool
    , error : Maybe String
    , requestId : Int
    , selected : Set String
    , quickLook : Maybe Int
    , inputFocused : Bool
    }


type Msg
    = UrlChanged Url.Url
    | UrlRequested Browser.UrlRequest
    | DraftChanged String
    | SearchSubmitted
    | SearchCompleted Int (Result Http.Error SearchResponse)
    | Retry
    | InputFocused
    | InputBlurred
    | ToggleSelected String
    | ClearSelection
    | OpenQuickLook PostSummary
    | CloseQuickLook
    | PreviousQuickLook
    | NextQuickLook
    | GlobalKey String


main : Program Flags Model Msg
main =
    Browser.application
        { init = init
        , onUrlChange = UrlChanged
        , onUrlRequest = UrlRequested
        , subscriptions = subscriptions
        , update = update
        , view = view
        }


init : Flags -> Url.Url -> Nav.Key -> ( Model, Cmd Msg )
init flags url key =
    let
        query = queryFromUrl url
        model =
            { key = key
            , apiBase = flags.apiBase
            , query = query
            , draftQuery = query
            , posts = []
            , loading = False
            , error = Nothing
            , requestId = 0
            , selected = Set.empty
            , quickLook = Nothing
            , inputFocused = False
            }
    in
    if String.trim query == "" then
        ( model, Cmd.none )

    else
        startSearch model query


subscriptions : Model -> Sub Msg
subscriptions model =
    if model.quickLook == Nothing then
        Sub.none

    else
        Browser.Events.onKeyDown (Decode.map GlobalKey (Decode.field "key" Decode.string))


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        UrlRequested request ->
            case request of
                Browser.Internal url ->
                    ( model, Nav.pushUrl model.key (urlHref url) )

                Browser.External href ->
                    ( model, Nav.load href )

        UrlChanged url ->
            let
                query = queryFromUrl url
            in
            if query == model.query then
                ( { model | draftQuery = query }, Cmd.none )

            else if String.trim query == "" then
                ( { model | query = "", draftQuery = "", posts = [], loading = False, error = Nothing, selected = Set.empty, quickLook = Nothing }, Cmd.none )

            else
                startSearch { model | query = query, draftQuery = query, selected = Set.empty, quickLook = Nothing } query

        DraftChanged query ->
            ( { model | draftQuery = query }, Cmd.none )

        SearchSubmitted ->
            let
                query = String.trim model.draftQuery
            in
            ( model, Nav.pushUrl model.key (queryHref query) )

        SearchCompleted id result ->
            if id /= model.requestId then
                ( model, Cmd.none )

            else
                case result of
                    Ok response ->
                        ( { model | posts = response.posts, loading = False, error = Nothing }, Cmd.none )

                    Err error ->
                        ( { model | posts = [], loading = False, error = Just (httpErrorToString error) }, Cmd.none )

        Retry ->
            if String.trim model.query == "" then
                ( model, Cmd.none )

            else
                startSearch model model.query

        InputFocused ->
            ( { model | inputFocused = True }, Cmd.none )

        InputBlurred ->
            ( { model | inputFocused = False }, Cmd.none )

        ToggleSelected id ->
            ( { model | selected = toggle id model.selected }, Cmd.none )

        ClearSelection ->
            ( { model | selected = Set.empty }, Cmd.none )

        OpenQuickLook post ->
            ( { model | quickLook = indexOf post.id model.posts }, Cmd.none )

        CloseQuickLook ->
            ( { model | quickLook = Nothing }, Cmd.none )

        PreviousQuickLook ->
            ( { model | quickLook = moveQuickLook -1 model.quickLook (List.length model.posts) }, Cmd.none )

        NextQuickLook ->
            ( { model | quickLook = moveQuickLook 1 model.quickLook (List.length model.posts) }, Cmd.none )

        GlobalKey key ->
            if key == "Escape" then
                ( { model | quickLook = Nothing }, Cmd.none )

            else if model.inputFocused then
                ( model, Cmd.none )

            else if key == "ArrowLeft" then
                ( { model | quickLook = moveQuickLook -1 model.quickLook (List.length model.posts) }, Cmd.none )

            else if key == "ArrowRight" then
                ( { model | quickLook = moveQuickLook 1 model.quickLook (List.length model.posts) }, Cmd.none )

            else
                ( model, Cmd.none )


startSearch : Model -> String -> ( Model, Cmd Msg )
startSearch model query =
    let
        id = model.requestId + 1
    in
    ( { model | query = query, draftQuery = query, loading = True, error = Nothing, requestId = id, posts = [], selected = Set.empty, quickLook = Nothing }
    , Api.Post.search model.apiBase query (SearchCompleted id)
    )


view : Model -> Browser.Document Msg
view model =
    { title = if String.trim model.query == "" then "Kura V3" else "Search · " ++ model.query
    , body = [ page model ]
    }


page : Model -> Html Msg
page model =
    div [ class "kura-shell" ]
        [ header [ class "kura-header" ]
            [ h1 [] [ text "Kura V3" ]
            , nav [] [ text "Media" ]
            ]
        , main_ [ class "kura-main" ]
            [ form [ class "search-panel", onSubmit SearchSubmitted ]
                [ input
                    [ class "search-input"
                    , placeholder "Search posts"
                    , value model.draftQuery
                    , onInput DraftChanged
                    , onFocus InputFocused
                    , onBlur InputBlurred
                    , attribute "aria-label" "Search posts"
                    ]
                    []
                , button [ class "button search-submit", type_ "submit" ] [ text "Search" ]
                ]
            , statusView model
            , Feature.Selection.controls model.selected (\_ -> ClearSelection)
            , if List.isEmpty model.posts then
                div [ class "empty-grid" ] []

              else
                Feature.MediaGrid.view model.apiBase model.posts model.selected OpenQuickLook ToggleSelected
            , quickLookView model
            ]
        ]


statusView : Model -> Html Msg
statusView model =
    case ( model.loading, model.error ) of
        ( True, _ ) ->
            p [ class "search-status" ] [ text "Searching…" ]

        ( False, Just error ) ->
            section [ class "search-status search-error" ]
                [ p [] [ text error ]
                , button [ class "button button-subtle", type_ "button", onClick Retry ] [ text "Retry" ]
                ]

        ( False, Nothing ) ->
            if String.trim model.query == "" then
                p [ class "search-status" ] [ text "Enter a search to browse media." ]

            else if List.isEmpty model.posts then
                p [ class "search-status" ] [ text "No posts found." ]

            else
                p [ class "search-status" ] [ text (String.fromInt (List.length model.posts) ++ " posts") ]


quickLookView : Model -> Html Msg
quickLookView model =
    case model.quickLook of
        Nothing ->
            div [] []

        Just index ->
            case itemAt index model.posts of
                Nothing ->
                    div [] []

                Just post ->
                    Feature.QuickLook.view model.apiBase post index (List.length model.posts) CloseQuickLook PreviousQuickLook NextQuickLook


queryFromUrl : Url.Url -> String
queryFromUrl url =
    case url.query of
        Nothing ->
            ""

        Just query ->
            queryValue "q" query


queryValue : String -> String -> String
queryValue key query =
    query
        |> String.split "&"
        |> List.filterMap (\part ->
            case String.split "=" part of
                name :: valueParts ->
                    if name == key then
                        Url.percentDecode (String.join "=" valueParts) |> Maybe.withDefault "" |> String.replace "+" " " |> Just

                    else
                        Nothing

                _ ->
                    Nothing
           )
        |> List.head
        |> Maybe.withDefault ""


queryHref : String -> String
queryHref query =
    if query == "" then
        Url.Builder.absolute [] []

    else
        Url.Builder.absolute [] [ Url.Builder.string "q" query ]


urlHref : Url.Url -> String
urlHref url =
    let
        query = Maybe.withDefault "" url.query
    in
    url.path ++ (if query == "" then "" else "?" ++ query) ++ (Maybe.map (\fragment -> "#" ++ fragment) url.fragment |> Maybe.withDefault "")


toggle : String -> Set String -> Set String
toggle id selected =
    if Set.member id selected then
        Set.remove id selected

    else
        Set.insert id selected


indexOf : String -> List PostSummary -> Maybe Int
indexOf id posts =
    posts
        |> List.indexedMap Tuple.pair
        |> List.filter (\( index, post ) -> post.id == id)
        |> List.head
        |> Maybe.map Tuple.first


itemAt : Int -> List a -> Maybe a
itemAt index items =
    items |> List.drop index |> List.head


moveQuickLook : Int -> Maybe Int -> Int -> Maybe Int
moveQuickLook delta current total =
    if total <= 0 then
        Nothing

    else
        case current of
            Nothing ->
                Just 0

            Just index ->
                Just (modBy total (index + delta + total))


httpErrorToString : Http.Error -> String
httpErrorToString error =
    case error of
        Http.BadUrl url ->
            "Invalid API URL: " ++ url

        Http.Timeout ->
            "The search timed out."

        Http.NetworkError ->
            "The search server could not be reached."

        Http.BadStatus status ->
            "Search failed (HTTP " ++ String.fromInt status ++ ")."

        Http.BadBody _ ->
            "The search response was invalid."
