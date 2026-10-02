module Main exposing (main)

import Browser
import Html exposing (Html, article, div, h1, header, img, input, main_, nav, p, section, text)
import Html.Attributes exposing (alt, class, height, placeholder, src, value, width)
import Html.Events exposing (onInput)
import Http
import Json.Decode as Decode exposing (Decoder)
import Url.Builder


apiBase : String
apiBase =
    "http://localhost:8080"


type alias PostSummary =
    { id : String
    , previewUrl : String
    , originalUrl : String
    , mediaType : String
    , width : Int
    , height : Int
    }


type alias SearchResponse =
    { posts : List PostSummary
    }


type alias Model =
    { query : String
    , posts : List PostSummary
    , loading : Bool
    , error : Maybe String
    }


type Msg
    = QueryChanged String
    | SearchCompleted (Result Http.Error SearchResponse)


main : Program () Model Msg
main =
    Browser.element
        { init = \_ -> ( initialModel, Cmd.none )
        , update = update
        , subscriptions = \_ -> Sub.none
        , view = view
        }


initialModel : Model
initialModel =
    { query = ""
    , posts = []
    , loading = False
    , error = Nothing
    }


update : Msg -> Model -> ( Model, Cmd Msg )
update msg model =
    case msg of
        QueryChanged query ->
            if String.trim query == "" then
                ( { model | query = query, posts = [], loading = False, error = Nothing }, Cmd.none )

            else
                ( { model | query = query, loading = True, error = Nothing }
                , searchPosts query
                )

        SearchCompleted result ->
            case result of
                Ok response ->
                    ( { model | posts = response.posts, loading = False, error = Nothing }, Cmd.none )

                Err error ->
                    ( { model | posts = [], loading = False, error = Just (httpErrorToString error) }, Cmd.none )


searchPosts : String -> Cmd Msg
searchPosts query =
    Http.get
        { url = Url.Builder.crossOrigin apiBase [ "api", "posts" ] [ Url.Builder.string "q" query ]
        , expect = Http.expectJson SearchCompleted searchResponseDecoder
        }


searchResponseDecoder : Decoder SearchResponse
searchResponseDecoder =
    Decode.map SearchResponse (Decode.field "posts" (Decode.list postSummaryDecoder))


postSummaryDecoder : Decoder PostSummary
postSummaryDecoder =
    Decode.map6 PostSummary
        (Decode.field "id" Decode.string)
        (Decode.field "preview_url" Decode.string)
        (Decode.field "original_url" Decode.string)
        (Decode.field "media_type" Decode.string)
        (Decode.field "width" Decode.int)
        (Decode.field "height" Decode.int)


view : Model -> Html Msg
view model =
    div [ class "kura-shell" ]
        [ header [ class "kura-header" ]
            [ h1 [] [ text "Kura V3" ]
            , nav [] [ text "Media" ]
            ]
        , main_ [ class "kura-main" ]
            [ section [ class "search-panel" ]
                [ input
                    [ class "search-input"
                    , placeholder "Search posts"
                    , value model.query
                    , onInput QueryChanged
                    ]
                    []
                ]
            , searchStatus model
            , mediaGrid model.posts
            ]
        ]


searchStatus : Model -> Html Msg
searchStatus model =
    case ( model.loading, model.error ) of
        ( True, _ ) ->
            p [ class "search-status" ] [ text "Searching…" ]

        ( False, Just error ) ->
            p [ class "search-status search-error" ] [ text error ]

        ( False, Nothing ) ->
            if String.trim model.query == "" then
                p [ class "search-status" ] [ text "Enter a search to browse media." ]

            else if List.isEmpty model.posts then
                p [ class "search-status" ] [ text "No posts found." ]

            else
                p [ class "search-status" ] []


mediaGrid : List PostSummary -> Html Msg
mediaGrid posts =
    section [ class "media-grid" ] (List.map postCard posts)


postCard : PostSummary -> Html Msg
postCard post =
    article [ class "media-card" ]
        [ img
            [ class "media-preview"
            , src post.previewUrl
            , alt ("Post " ++ post.id)
            , width post.width
            , height post.height
            ]
            []
        , p [ class "media-meta" ]
            [ text (post.mediaType ++ " · " ++ String.fromInt post.width ++ "×" ++ String.fromInt post.height) ]
        ]


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
